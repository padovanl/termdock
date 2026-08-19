package server

import (
	"encoding/gob"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"termdock/internal/proto"
)

const probeTimeout = 300 * time.Millisecond

// Probe connects to a session's socket and asks for its info. Returns
// false if nothing is listening there (e.g. a stale socket left behind by
// a crashed daemon).
func Probe(sockPath string) (proto.ServerInfo, bool) {
	conn, err := net.DialTimeout("unix", sockPath, probeTimeout)
	if err != nil {
		return proto.ServerInfo{}, false
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(probeTimeout))

	if err := gob.NewEncoder(conn).Encode(proto.ClientMsg{Kind: "query"}); err != nil {
		return proto.ServerInfo{}, false
	}
	var reply proto.ServerMsg
	if err := gob.NewDecoder(conn).Decode(&reply); err != nil || reply.Kind != "info" || reply.Info == nil {
		return proto.ServerInfo{}, false
	}
	return *reply.Info, true
}

// List enumerates every session found in the socket directory, probing
// each one and pruning any stale socket files it finds along the way.
func List() ([]proto.ServerInfo, error) {
	dir, err := Dir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []proto.ServerInfo
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sock") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		info, ok := Probe(path)
		if !ok {
			os.Remove(path)
			continue
		}
		out = append(out, info)
	}
	return out, nil
}

// Kill asks a running session to shut down.
func Kill(sockPath string) error {
	conn, err := net.DialTimeout("unix", sockPath, probeTimeout)
	if err != nil {
		return err
	}
	defer conn.Close()
	return gob.NewEncoder(conn).Encode(proto.ClientMsg{Kind: "kill"})
}
