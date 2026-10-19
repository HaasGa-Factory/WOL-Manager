//go:build linux

package main

import (
	"encoding/json"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"
)

func TestStateAtomicWritePreservesDataAndServiceAccess(t *testing.T) {
	dir := testDir(t)
	path := filepath.Join(dir, "state.json")
	now := time.Now().UTC()
	original := jobFile{Jobs: map[string]*Job{
		"upgrade-job": {ID: "upgrade-job", Kind: "test", Target: "preserve", State: "completed", Created: now, Updated: now},
	}}
	content, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}
	store, err := openJobs(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.setCycle(PowerCycle{UPS: "ups-test", State: "armed"}); err != nil {
		t.Fatal(err)
	}
	reloaded, err := openJobs(path)
	if err != nil {
		t.Fatal(err)
	}
	if job, ok := reloaded.get("upgrade-job"); !ok || job.Target != "preserve" {
		t.Fatalf("job perdu pendant l'écriture atomique: %#v", job)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0640 {
		t.Fatalf("mode state.json = %04o, attendu 0640", info.Mode().Perm())
	}
	stat := info.Sys().(*syscall.Stat_t)
	wantUID := uint32(os.Geteuid())
	wantGID := uint32(os.Getegid())
	if os.Geteuid() == 0 {
		if account, lookupErr := user.Lookup("wol-manager"); lookupErr == nil {
			uid, parseErr := strconv.ParseUint(account.Uid, 10, 32)
			if parseErr != nil {
				t.Fatal(parseErr)
			}
			wantUID = uint32(uid)
			gid, parseErr := strconv.ParseUint(account.Gid, 10, 32)
			if parseErr != nil {
				t.Fatal(parseErr)
			}
			wantGID = uint32(gid)
		}
	}
	if stat.Uid != wantUID || stat.Gid != wantGID {
		t.Fatalf("propriétaire state.json = %d:%d, attendu %d:%d", stat.Uid, stat.Gid, wantUID, wantGID)
	}
}
