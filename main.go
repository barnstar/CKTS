package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"tailscale.com/tsnet"
)

func main() {
	var (
		playlist = flag.String("playlist", "", "path to playlist file (newline-separated MP3 paths)")
		lineIn   = flag.Bool("linein", false, "stream from ALSA line-in (requires arecord)")
		alsaDev  = flag.String("alsa-device", "default", "ALSA capture device name")
		hostname = flag.String("hostname", "CKTS-Radio", "tsnet hostname")
		authKey  = flag.String("authkey", "", "tailscale auth key (optional)")
		local    = flag.Bool("local", false, "listen on a local TCP address instead of tsnet")
		addr     = flag.String("addr", ":8080", "listen address when -local is set")
		autoplay = flag.Bool("autoplay", false, "start streaming immediately on launch")
	)
	flag.Parse()

	if *playlist == "" && !*lineIn {
		fmt.Fprintln(os.Stderr, "error: specify -playlist <file> or -linein")
		flag.Usage()
		os.Exit(1)
	}
	if *playlist != "" && *lineIn {
		fmt.Fprintln(os.Stderr, "error: -playlist and -linein are mutually exclusive")
		flag.Usage()
		os.Exit(1)
	}

	hub := NewHub()

	var src AudioSource
	if *playlist != "" {
		src = NewPlaylistSource(*playlist, hub)
	} else {
		src = NewLineInSource(*alsaDev, hub)
	}

	srv := NewServer(hub, src)

	// Graceful shutdown.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Println("shutting down...")
		src.Stop()
		os.Exit(0)
	}()

	// Start streaming immediately if requested.
	if *autoplay {
		if err := src.Start(); err != nil {
			log.Fatalf("autoplay start: %v", err)
		}
	}

	var ln net.Listener
	var err error

	if *local {
		ln, err = net.Listen("tcp", *addr)
		if err != nil {
			log.Fatalf("listen %s: %v", *addr, err)
		}
		log.Printf("CKTS listening at http://%s", *addr)
	} else {
		ts := &tsnet.Server{
			Hostname: *hostname,
			AuthKey:  *authKey,
		}
		ln, err = ts.Listen("tcp", ":80")
		if err != nil {
			log.Fatalf("tsnet listen: %v", err)
		}
		log.Printf("CKTS listening on tsnet hostname %q", *hostname)
	}

	if err := http.Serve(ln, srv.Router()); err != nil {
		log.Fatalf("http serve: %v", err)
	}
}
