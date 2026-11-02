package chrona

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"
)

func TestSSHTargetSyntaxAndOptionalPorts(t *testing.T) {
	for _, tc := range []struct {
		input string
		port  int
		host  string
		want  int
	}{
		{"nas-pve", 0, "nas-pve", 0},
		{"nas-pve", 2202, "nas-pve", 2202},
		{"backup.example:2222", 0, "backup.example", 2222},
		{"root@backup.example:2222", 2222, "root@backup.example", 2222},
		{"[2001:db8::1]:2202", 0, "2001:db8::1", 2202},
		{"backup@[2001:db8::1]", 0, "backup@2001:db8::1", 0},
		{"2001:db8::1", 0, "2001:db8::1", 0},
		{"local", 0, "local", 0},
	} {
		t.Run(tc.input, func(t *testing.T) {
			host, port, err := parseSSHTarget(tc.input, tc.port)
			if err != nil || host != tc.host || port != tc.want {
				t.Fatal(host, port, err)
			}
		})
	}
	for _, tc := range []struct {
		host string
		port int
	}{
		{"", 0}, {"-Fconfig", 0}, {"bad host", 0}, {"host;id", 0},
		{"user@-host", 0}, {"user@user@host", 0}, {"user;id@host", 0},
		{"example:0", 0}, {"example:65536", 0}, {"example:22", 2202},
		{"example:", 0}, {"example:+22", 0}, {"example:abc", 0},
		{"example:22:33", 0}, {"[bad]:22", 0}, {"[2001:db8::1]junk", 0},
		{"[2001:db8::1]:", 0}, {"example", -1}, {"example", 65536},
	} {
		if _, _, err := parseSSHTarget(tc.host, tc.port); err == nil {
			t.Fatal("invalid target accepted", tc)
		}
	}
}

func TestSSHUsesOpenSSHConfigurationWithoutPortOverride(t *testing.T) {
	if _, err := exec.LookPath("ssh"); err != nil {
		t.Skip("OpenSSH client unavailable")
	}
	dir := t.TempDir()
	key := filepath.Join(dir, "test-identity")
	include := filepath.Join(dir, "included.conf")
	config := filepath.Join(dir, "ssh.conf")
	if err := os.WriteFile(include, []byte("Host nas-pve\n HostName 192.0.2.10\n Port 2202\n User nas-user\n IdentityFile "+key+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config, []byte("Include "+include+"\nHost alias-*\n User wildcard-user\n IdentityFile "+key+"\nMatch originalhost alias-matched\n Port 2244\nHost *\n CanonicalizeHostname no\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		target, host, user, port string
		override                 int
	}{
		{"nas-pve", "192.0.2.10", "nas-user", "2202", 0},
		{"alias-matched", "alias-matched", "wildcard-user", "2244", 0},
		{"unmatched.invalid", "unmatched.invalid", "", "22", 0},
		{"nas-pve", "192.0.2.10", "nas-user", "2233", 2233},
		{"root@nas-pve", "192.0.2.10", "root", "2202", 0},
	} {
		t.Run(tc.target+"-"+tc.port, func(t *testing.T) {
			command := (nativeEndpoint{host: tc.target, port: tc.override}).command(t.Context(), "list", "-Hp", "tank/data")
			hasPort := false
			for _, arg := range command.Args[1:] {
				hasPort = hasPort || arg == "-p"
			}
			if hasPort != (tc.override != 0) {
				t.Fatal("empty port unexpectedly overrode SSH config", command.Args)
			}
			// -G evaluates the real OpenSSH parser without connecting or reading keys.
			args := append([]string{"-G", "-F", config}, command.Args[1:]...)
			out, err := exec.CommandContext(t.Context(), "ssh", args...).CombinedOutput()
			if err != nil {
				t.Fatal(err, string(out))
			}
			values := map[string]string{}
			for _, line := range strings.Split(string(out), "\n") {
				key, value, ok := strings.Cut(line, " ")
				if ok {
					values[key] = value
				}
			}
			if values["hostname"] != tc.host || values["port"] != tc.port || (tc.user != "" && values["user"] != tc.user) {
				t.Fatal(values)
			}
			if tc.user != "" && values["identityfile"] != key {
				t.Fatal("IdentityFile was not preserved", values["identityfile"])
			}
		})
	}
}

func TestOptionalPortConfigRoundTripPreservesExplicitLegacyPort(t *testing.T) {
	c := DefaultConfig(time.Now())
	c.Backups[0].Port = 2202
	c.Backups[1].Host, c.Backups[1].Port = "local", 0
	data, err := yaml.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	opts := Options{Mode: "demo", ConfigPath: filepath.Join(dir, "config.yaml"), StatePath: filepath.Join(dir, "state.json")}
	if err := os.WriteFile(opts.ConfigPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	a, err := NewApp(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	after, _ := os.ReadFile(opts.ConfigPath)
	if string(data) != string(after) || a.config.Backups[0].Port != 2202 || a.config.Backups[1].Port != 0 {
		t.Fatal("loading changed explicit or unspecified ports")
	}
	encoded, _ := json.Marshal(a.config)
	var decoded struct {
		Backups []map[string]any `json:"backups"`
	}
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Backups[0]["port"] != float64(2202) {
		t.Fatal("explicit port lost")
	}
	if _, exists := decoded.Backups[1]["port"]; exists {
		t.Fatal("empty port serialized")
	}
	c.Backups[0].Host, c.Backups[0].Port = "backup.example:2244", 0
	c = NormalizeConfig(c)
	if err := ValidateConfig(c); err != nil || c.Backups[0].Host != "backup.example" || c.Backups[0].Port != 2244 {
		t.Fatal("inline endpoint not normalized", c.Backups[0], err)
	}
	c.Backups[1].Datasets = c.Backups[1].Datasets[:1]
	c.Backups[1].Datasets[0] = c.Backups[0].Datasets[0]
	c.Backups[1].Host, c.Backups[1].Port = "backup.example:2244", 0
	if err := validateEffectiveBackupTargets(c, nil); err == nil {
		t.Fatal("equivalent inline/separate targets bypassed duplicate check")
	}
}

func TestClearingLocalPortPreservesIncrementalHistoryAndProtection(t *testing.T) {
	a := testApp(t)
	c := NormalizeConfig(a.config)
	c.Backups[1].Host, c.Backups[1].Port = "local", 22
	if err := a.applyConfigLocked(c); err != nil {
		t.Fatal(err)
	}
	b := a.config.Backups[1]
	if err := a.finishDemoBackupLocked(b, time.Now()); err != nil {
		t.Fatal(err)
	}
	before := a.state.Backups[b.ID]
	c = NormalizeConfig(a.config)
	c.Backups[1].Port = 0
	if err := a.applyConfigLocked(c); err != nil {
		t.Fatal(err)
	}
	after := a.state.Backups[b.ID]
	if !reflect.DeepEqual(before.Bases, after.Bases) || !reflect.DeepEqual(before.Completed, after.Completed) || after.Status != "success" {
		t.Fatal("irrelevant local port changed backup history")
	}
	for _, binding := range b.Datasets {
		base := after.Bases[binding.Source]
		r := findReplica(a.state.Replicas, a.config.Backups[1], binding, base)
		if r == nil || len(r.HeldBy) == 0 || !a.protectedSnapshotNamesLocked()[base] {
			t.Fatal("local port edit lost common-base protection")
		}
	}
	if err := a.finishDemoBackupLocked(a.config.Backups[1], time.Now()); err != nil {
		t.Fatal(err)
	}
	for _, binding := range b.Datasets {
		if a.state.Backups[b.ID].Transfers[binding.Source].Mode != "incremental" {
			t.Fatal("clearing local port forced full send")
		}
	}
	if sameBackupReceiver("nas-pve", 2202, "nas-pve", 0) {
		t.Fatal("unresolved SSH config assumed to have the same explicit port")
	}
	if (a.endpoint(Backup{Host: "local", Port: 0})).host != "" {
		t.Fatal("local still attempted SSH")
	}
}
