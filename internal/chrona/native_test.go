package chrona

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeBaseRequiresLatestReceiverGUID(t *testing.T) {
	local := []Snapshot{{Name: "tank/src@old", GUID: "1"}, {Name: "tank/src@new", GUID: "2"}}
	remote := []Snapshot{{Name: "backup/dst@renamed", GUID: "2"}}
	base, err := selectNativeBase(local, remote)
	if err != nil || base.Name != "tank/src@new" {
		t.Fatal("GUID matching must tolerate different snapshot names", base, err)
	}
	remote = append(remote, Snapshot{Name: "backup/dst@independent", GUID: "3"})
	if _, err = selectNativeBase(local, remote); err == nil {
		t.Fatal("unrelated newer receiver snapshot must never be rolled back automatically")
	}
	if base, err = selectNativeBase(local, nil); err != nil || base != nil {
		t.Fatal("a new receiver must use full send")
	}
}

func TestWebAuthenticationProtectsReadsAndWrites(t *testing.T) {
	secret := "a-long-production-password-for-tests"
	h := authorize(secret, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	for _, method := range []string{"GET", "PUT"} {
		for _, password := range []string{"", "wrong", secret} {
			r := httptest.NewRequest(method, "http://chrona/api/config", nil)
			if password != "" {
				r.SetBasicAuth("chrona", password)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if password == secret {
				if w.Code != 204 {
					t.Fatal(w.Code)
				}
			} else if w.Code != 401 || w.Header().Get("WWW-Authenticate") == "" {
				t.Fatal("unauthorized request reached handler", w.Code)
			}
		}
	}
}

func TestRealModeRejectsDemoPersistence(t *testing.T) {
	a := testApp(t)
	a.opts.Mode = "zfs"
	if _, err := NewApp(a.opts); err == nil || !strings.Contains(err.Error(), "演示状态") {
		t.Fatal("fake successful backups must never enter real mode", err)
	}
}

func TestAllDatasetRowsDoNotEnableUnconfiguredDatasets(t *testing.T) {
	a := testApp(t)
	all := a.visibleDatasetsLocked()
	if len(all) != len(a.datasets) {
		t.Fatal("inventory datasets missing", len(all), len(a.datasets))
	}
	before := len(a.config.EffectivePolicies(a.datasets...))
	found := false
	for _, d := range all {
		if d.Name == "Saturn/APP/Seafile" {
			found = true
		}
	}
	if !found || a.config.IsManagedDataset("Saturn/APP/Seafile") {
		t.Fatal("showing all datasets must not register them")
	}
	if len(a.config.EffectivePolicies(a.datasets...)) != before {
		t.Fatal("viewing inventory enabled snapshot scheduling")
	}
}

func TestSSHCommandUsesQuotedArgumentsAndPersistentConnections(t *testing.T) {
	e := nativeEndpoint{host: "backup", port: 2202, controlDir: "/run/chrona/ssh"}
	c := e.command(t.Context(), "get", "-Hp", "-o", "value", ExpiryProperty, "tank/a@snap")
	joined := strings.Join(c.Args, " ")
	if !strings.Contains(joined, "ControlMaster=auto") || !strings.Contains(joined, "ControlPersist=600") || !strings.Contains(joined, "StrictHostKeyChecking=yes") {
		t.Fatal(joined)
	}
	if !strings.Contains(joined, "'tank/a@snap'") || shellArg("a'b") != "'a'\"'\"'b'" {
		t.Fatal("remote arguments not safely quoted", joined)
	}
}

func TestStatePathAndAuthFilesRemainPrivate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	if err := saveState(path, State{Mode: "zfs"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("state permissions", err, info)
	}
}
