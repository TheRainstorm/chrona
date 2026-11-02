package chrona

import (
	"errors"
	"net"
	"regexp"
	"strconv"
	"strings"
)

var sshUserPattern = regexp.MustCompile(`^[a-zA-Z0-9_][a-zA-Z0-9_.-]{0,63}$`)
var sshHostPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,180}$`)

// parseSSHTarget handles address syntax only. OpenSSH itself resolves Host,
// Include, Match, User, Port and IdentityFile from the service account's config.
// Port zero deliberately means no command-line override, not an explicit 22.
func parseSSHTarget(target string, port int) (string, int, error) {
	if port < 0 || port > 65535 {
		return "", 0, errors.New("SSH 端口应留空或填写 1～65535")
	}
	user, host := "", target
	if i := strings.IndexByte(host, '@'); i >= 0 {
		user, host = host[:i], host[i+1:]
		if !sshUserPattern.MatchString(user) {
			return "", 0, errors.New("SSH 用户名无效")
		}
	}
	inline := ""
	if strings.HasPrefix(host, "[") {
		end := strings.IndexByte(host, ']')
		if end < 0 || net.ParseIP(host[1:end]) == nil {
			return "", 0, errors.New("IPv6 地址应使用 [地址] 或 [地址]:端口")
		}
		tail := host[end+1:]
		if tail != "" {
			if !strings.HasPrefix(tail, ":") || len(tail) == 1 {
				return "", 0, errors.New("IPv6 地址后的端口无效")
			}
			inline = tail[1:]
		}
		host = host[1:end]
	} else if strings.Count(host, ":") == 1 {
		i := strings.IndexByte(host, ':')
		host, inline = host[:i], host[i+1:]
		if inline == "" {
			return "", 0, errors.New("主机冒号后需要填写端口")
		}
	}
	if inline != "" {
		for _, digit := range inline {
			if digit < '0' || digit > '9' {
				return "", 0, errors.New("SSH 端口必须是 1～65535 的整数")
			}
		}
		n, err := strconv.Atoi(inline)
		if err != nil || n < 1 || n > 65535 {
			return "", 0, errors.New("SSH 端口必须是 1～65535 的整数")
		}
		if port != 0 && port != n {
			return "", 0, errors.New("主机中指定的端口与单独填写的端口不一致")
		}
		port = n
	}
	if !sshHostPattern.MatchString(host) && net.ParseIP(host) == nil {
		return "", 0, errors.New("请填写 SSH 别名、域名或 IP 地址，可附加 :端口")
	}
	if user != "" {
		host = user + "@" + host
	}
	return host, port, nil
}

func normalizeBackupTarget(b Backup) Backup {
	if host, port, err := parseSSHTarget(b.Host, b.Port); err == nil {
		b.Host, b.Port = host, port
	}
	return b
}

func literalLocalHost(host string) bool {
	return host == "local" || host == "localhost" || host == "127.0.0.1"
}

type backupReceiverAddress struct {
	host string
	port int
}

// Local transfers never use a port. Canonical comparison preserves old local
// bases when the now-optional port is cleared, without guessing remote aliases.
func backupReceiver(host string, port int) backupReceiverAddress {
	if name, number, err := parseSSHTarget(host, port); err == nil {
		host, port = name, number
	}
	if literalLocalHost(host) {
		host, port = "local", 0
	}
	return backupReceiverAddress{host: host, port: port}
}

func sameBackupReceiver(aHost string, aPort int, bHost string, bPort int) bool {
	return backupReceiver(aHost, aPort) == backupReceiver(bHost, bPort)
}
