package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

func runParent() {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	hostname := fs.String("hostname", "nanobox", "Sets the hostname of the container")
	rootfs := fs.String("rootfs", "./rootfs", "Path to container rootfilesystem")
	memLimit := fs.String("memory", "20971520", "memory limit in bytes")
	fs.Parse(os.Args[2:])

	args := fs.Args()
	if len(args) == 0 {
		log.Fatal("Please specify the command to run e.g. /bin/bash\n")
	}

	cmd := exec.Command("/proc/self/exe", append([]string{"child", *hostname, *rootfs}, args...)...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags: unix.CLONE_NEWPID | unix.CLONE_NEWUTS | unix.CLONE_NEWNS,
	}

	if err := cmd.Start(); err != nil {
		log.Fatalf("Failed to start child process: %v\n", err)
	}

	parentCGroup, err := getCurrentCGroupPath()
	if err != nil {
		log.Fatalf("Failed to get current cgroup: %v\n", err)
	}
	// Enable memory and cpu controllers in parent cgroup
	var cGroupPath string
	parentPath := filepath.Join("/sys/fs/cgroup", parentCGroup)
	if err := os.WriteFile(filepath.Join(parentPath, "cgroup.subtree_control"), []byte("+memory +cpu"), 0644); err != nil {
		log.Printf("[Parent] Cannot enable controllers in parent, falling back to root cgroup: %v", err)
		// Fallback
		cGroupPath = "/sys/fs/cgroup/nanobox"
	} else {
		cGroupPath = filepath.Join(parentPath, "nanobox")
	}

	if err := os.MkdirAll(cGroupPath, 0755); err != nil {
		log.Fatalf("[Parent] Failed to create cgroup dir: %v\n", err)
	}

	if err := os.WriteFile(filepath.Join(cGroupPath, "memory.max"), []byte(*memLimit), 0644); err != nil {
		log.Fatalf("[Parent] Failed to WriteFile memory.max: %v\n", err)
	}

	if err := os.WriteFile(filepath.Join(cGroupPath, "cgroup.procs"), []byte(strconv.Itoa(cmd.Process.Pid)), 0644); err != nil {
		log.Fatalf("[Parent] Failed to WriteFile cgroup.procs: %v", err)
	}

	defer func() {
		if err := os.Remove(cGroupPath); err != nil {
			log.Fatalf("[Parent] Failed to cleanup: %v\n", err)
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		for sig := range sigChan {
			if cmd.Process != nil {
				_ = cmd.Process.Signal(sig)
			}
		}
	}()

	err = cmd.Wait()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
				fmt.Printf("\n[Parent] Child exited with code: %d\n", status.ExitStatus())
				os.Exit(status.ExitStatus())
			}
		}
		log.Fatalf("\n[Parent] Child crashed: %v\n", err)
	}
	fmt.Println("\n[Parent] Child completed successfully.")
}

func runChild() {
	if len(os.Args) < 4 {
		log.Fatal("[Child] missing hostname or command")
	}
	hostname := os.Args[2]
	newRoot := os.Args[3]
	args := os.Args[4:]

	fmt.Printf("[Child] booting namespaces: Current PID = %d\n", os.Getpid())

	err := unix.Sethostname([]byte(hostname))
	if err != nil {
		log.Fatalf("[Child] Failed set hostname: %v\n", err)
	}
	if len(args) == 0 {
		log.Fatal("[Child] No commands specified to execute\n")
	}

	putOld := filepath.Join(newRoot, ".old_root") //pivot_root requires a place to put the host root

	if err = os.MkdirAll(putOld, 0700); err != nil {
		log.Fatalf("[Child] Failed to create put_old dir: %v\n", err)
	}

	// bind mount
	if err = unix.Mount("", "/", "", unix.MS_PRIVATE|unix.MS_REC, ""); err != nil {
		log.Fatalf("[Child] Mount failed: %v\n", err)
	}

	if err = unix.Mount(newRoot, newRoot, "", unix.MS_BIND, ""); err != nil {
		log.Fatalf("[Child] Failed bind mount newRoot: %v\n", err)
	}

	if err = unix.PivotRoot(newRoot, putOld); err != nil {
		log.Fatalf("[Child] Pivot Root failed: %v\n", err)
	}

	// Change the pwd to the new root
	if err = unix.Chdir("/"); err != nil {
		log.Fatalf("[Child] Failed to chdir to root: %v\n", err)
	}

	if err = unix.Mount("proc", "/proc", "proc", 0, ""); err != nil {
		log.Fatalf("[Child] Failed to mount /proc: %v\n", err)
	}

	if err = unix.Unmount("/.old_root", unix.MNT_DETACH); err != nil {
		log.Fatalf("[Child] Failed to unmount to old root: %v\n", err)
	}

	os.Remove("/.old_root")

	binaryPath, err := exec.LookPath(args[0])
	if err != nil {
		log.Fatalf("[Child] Command not found in PATH: %v\n", err)
	}
	if err := unix.Exec(binaryPath, args, []string{"PATH=/bin:/usr/bin"}); err != nil {
		log.Fatalf("[Child] Failed to execute binary: %v\n", err)
	}
}

func getCurrentCGroupPath() (string, error) {
	data, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return "", err
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "0::") {
			return strings.TrimPrefix(line, "0::"), nil
		}
	}
	return "", fmt.Errorf("cgroup v2 not found")
}
