# nanobox
a minimal Linux container runtime that creates isolated execution environments using namespaces and cgroups.

## Phase 1
Entering new PID and UTS namespaces, setting a hostname, executing a command, and handling shutdown signal.
`clone` is a linux system call to create a process. It is more efficeint than `fork`
`fork` creates a child that is an exact copy of the parent (same address space, same file descriptor table, same namespace memberships). `clone` allows you to choose which parts the child shares with the parent.

Namespaces are one of the things you can make private. When you pass a flag like `CLONE_NEWPID`, the kernel creates a new PID namespace for the child. Inside that namespace, the child sees its own process IDs as starting from 1 (it becomes PID 1). The parent still sees the child with its original global PID. This is the foundation of containers – a process thinks it is alone on a system.

Similarly, `CLONE_NEWUTS` gives the child its own UTS namespace (hostname, domainname). That allows you to sethostname inside the container without affecting the host.

We will use the `unix.Clone` function from `golang.org/x/sys/unix`, which handles the tricky stack setup required by clone. It takes a child function that returns an exit code, a stack buffer, and the flags.

Why we need a separate stack: The raw clone syscall expects a new stack pointer for the child. If we used the parent’s stack directly, the child would corrupt it. `unix.Clone` allocates the stack for us and jumps to our child function safely.

#### Phase 1 goals:
1. Parses a hostname and command (with arguments) from commandline flags
2. Create a child process with its own PID and UTS namespaces
3. Inside the child process, set hostname and then executes the given command.
4. In the parent, waits for the child and prints its exit status.
5. On SIGINT/SIGTERM, the parent forwards the signal to the child, waits for it to finish, and exits cleanly.