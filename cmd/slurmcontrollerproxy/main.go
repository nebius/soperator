package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"time"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/slurmproxy"
)

func main() {
	var (
		listenAddress  string
		jwtKeyFile     string
		allowedUsers   string
		scontrolPath   string
		commandTimeout time.Duration
	)

	flag.StringVar(&listenAddress, "listen", slurmproxy.DefaultListenAddress, "HTTP listen address")
	flag.StringVar(&jwtKeyFile, "jwt-key-file", consts.VolumeMountPathRESTJWTKey+"/"+consts.SecretRESTJWTKeyFileName, "JWT signing key file")
	flag.StringVar(&allowedUsers, "allowed-users", slurmproxy.DefaultAllowedUsers, "Comma-separated Slurm usernames allowed to call privileged RPCs")
	flag.StringVar(&scontrolPath, "scontrol-path", "/usr/bin/scontrol", "Path to scontrol")
	flag.DurationVar(&commandTimeout, "command-timeout", 30*time.Second, "Timeout for each privileged Slurm command")
	flag.Parse()

	jwtKey, err := os.ReadFile(jwtKeyFile)
	if err != nil {
		log.Fatalf("read jwt key file: %v", err)
	}

	server, err := slurmproxy.NewServer(slurmproxy.ServerOptions{
		JWTKey:         jwtKey,
		AllowedUsers:   allowedUsers,
		ScontrolPath:   scontrolPath,
		CommandTimeout: commandTimeout,
	})
	if err != nil {
		log.Fatalf("create server: %v", err)
	}

	log.Printf("starting slurm controller proxy on %s", listenAddress)
	if err := http.ListenAndServe(listenAddress, server.Handler()); err != nil {
		log.Fatalf("serve: %v", err)
	}
}
