package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"golang.org/x/sys/unix"
)

func runParent(hostname string, args []string) {
	if len(args) == 0 {
		log.Fatal("Please specify the command to run e.g. /bin/bash\n")
	}

	cmd := exec.Command("/proc/self/exe", append([]string{"-hostname", hostname}, args...)...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), "NANOBOX_CHILD=1")

	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags: unix.CLONE_NEWPID | unix.CLONE_NEWUTS | unix.CLONE_NEWNS,
	}

	if err := cmd.Start(); err != nil {
		log.Fatalf("Failed to start child process: %v\n", err)
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-sigChan
		if cmd.Process != nil {
			_ = cmd.Process.Signal(sig)
		}
	}()

	err := cmd.Wait()
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

func runChild(hostname string, args []string) {
	fmt.Printf("[Child] booting namespaces: Current PID = %d\n", os.Getpid())
	err := unix.Sethostname([]byte(hostname))
	if err != nil {
		log.Fatalf("[Child] Failed set hostname: %v\n", err)
	}
	if len(args) == 0 {
		log.Fatal("[Child] No commands specified to execute\n")
	}

	newRoot := "/home/peter/nanobox/rootfs"
	putOld := newRoot + "/.old_root" //pivot_root requires a place to put the host root

	if err = os.MkdirAll(putOld, 0700); err != nil {
		log.Fatalf("[Child] Failed to create put_old dir: %v\n", err)
	}

	// bind mount
	if err = unix.Mount("", "/", "", unix.MS_PRIVATE|unix.MS_REC, ""); err != nil {
		log.Fatalf("[Child] Mount failed: %v\n", err)
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

	binaryPath, err := exec.LookPath(args[0])
	if err != nil {
		log.Fatalf("[Child] Command not found in PATH: %v\n", err)
	}
	if err := unix.Exec(binaryPath, args, []string{"PATH=/bin:/usr/bin"}); err != nil {
		log.Fatalf("[Child] Failed to execute binary: %v\n", err)
	}
}
