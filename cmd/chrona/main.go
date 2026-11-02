package main

import (
	"context"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"homelab/chrona/internal/chrona"
	"homelab/chrona/web"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:8086", "Web listen address")
	config := flag.String("config", "data/config.yaml", "YAML configuration path")
	state := flag.String("state", "", "Demo runtime state path (default: beside config)")
	mode := flag.String("mode", "demo", "demo or zfs (read-only by default)")
	authFile := flag.String("auth-file", "", "File containing the Web password (user: chrona)")
	sshControlDir := flag.String("ssh-control-dir", "", "Private directory for reusable SSH connections")
	writes := flag.Bool("enable-zfs-writes", false, "Explicitly allow creating and pruning Chrona snapshots in zfs mode")
	flag.Parse()
	if *state == "" {
		name := "demo-state.json"
		if *mode == "zfs" {
			name = "state.json"
		}
		*state = filepath.Join(filepath.Dir(*config), name)
	}
	secret := ""
	if *authFile != "" {
		value, err := os.ReadFile(*authFile)
		if err != nil {
			log.Fatal(err)
		}
		secret = strings.TrimSpace(string(value))
		if len(secret) < 20 {
			log.Fatal("Web password must contain at least 20 characters")
		}
	}
	app, err := chrona.NewApp(chrona.Options{ConfigPath: *config, StatePath: *state, Mode: *mode, AllowWrites: *writes, AuthSecret: secret, SSHControlDir: *sshControlDir})
	if err != nil {
		log.Fatal(err)
	}
	defer app.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	listener, err := net.Listen("tcp", *listen)
	if err != nil {
		log.Fatal(err)
	}
	server := &http.Server{Handler: app.Handler(web.Assets), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 45 * time.Second, IdleTimeout: 60 * time.Second}
	go app.Run(ctx)
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	log.Printf("Chrona 0.10.1 · %s mode · http://%s · config %s", *mode, listener.Addr(), *config)
	if err = server.Serve(listener); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
