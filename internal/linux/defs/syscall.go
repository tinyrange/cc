package defs

import "fmt"

type Syscall int

const (
	SYS_READ Syscall = iota
	SYS_WRITE
	SYS_CLOSE
	SYS_FSTAT
	SYS_LSEEK
	SYS_MMAP
	SYS_MPROTECT
	SYS_MUNMAP
	SYS_BRK
	SYS_RT_SIGACTION
	SYS_RT_SIGPROCMASK
	SYS_RT_SIGRETURN
	SYS_IOCTL
	SYS_PREAD64
	SYS_PWRITE64
	SYS_READV
	SYS_WRITEV
	SYS_SCHED_YIELD
	SYS_MREMAP
	SYS_MSYNC
	SYS_MINCORE
	SYS_MADVISE
	SYS_SHMGET
	SYS_SHMAT
	SYS_SHMCTL
	SYS_DUP
	SYS_NANOSLEEP
	SYS_GETITIMER
	SYS_SETITIMER
	SYS_GETPID
	SYS_SENDFILE
	SYS_SOCKET
	SYS_CONNECT
	SYS_ACCEPT
	SYS_SENDTO
	SYS_RECVFROM
	SYS_SENDMSG
	SYS_RECVMSG
	SYS_SHUTDOWN
	SYS_BIND
	SYS_LISTEN
	SYS_GETSOCKNAME
	SYS_GETPEERNAME
	SYS_SOCKETPAIR
	SYS_SETSOCKOPT
	SYS_GETSOCKOPT
	SYS_CLONE
	SYS_EXECVE
	SYS_EXIT
	SYS_WAIT4
	SYS_KILL
	SYS_UNAME
	SYS_SEMGET
	SYS_SEMOP
	SYS_SEMCTL
	SYS_SHMDT
	SYS_MSGGET
	SYS_MSGSND
	SYS_MSGRCV
	SYS_MSGCTL
	SYS_FCNTL
	SYS_FLOCK
	SYS_FSYNC
	SYS_FDATASYNC
	SYS_TRUNCATE
	SYS_FTRUNCATE
	SYS_GETCWD
	SYS_CHDIR
	SYS_FCHDIR
	SYS_FCHMOD
	SYS_FCHOWN
	SYS_UMASK
	SYS_GETTIMEOFDAY
	SYS_GETRLIMIT
	SYS_GETRUSAGE
	SYS_SYSINFO
	SYS_TIMES
	SYS_PTRACE
	SYS_GETUID
	SYS_SYSLOG
	SYS_GETGID
	SYS_SETUID
	SYS_SETGID
	SYS_GETEUID
	SYS_GETEGID
	SYS_SETPGID
	SYS_GETPPID
	SYS_SETSID
	SYS_SETREUID
	SYS_SETREGID
	SYS_GETGROUPS
	SYS_SETGROUPS
	SYS_SETRESUID
	SYS_GETRESUID
	SYS_SETRESGID
	SYS_GETRESGID
	SYS_GETPGID
	SYS_SETFSUID
	SYS_SETFSGID
	SYS_GETSID
	SYS_CAPGET
	SYS_CAPSET
	SYS_RT_SIGPENDING
	SYS_RT_SIGTIMEDWAIT
	SYS_RT_SIGQUEUEINFO
	SYS_RT_SIGSUSPEND
	SYS_SIGALTSTACK
	SYS_PERSONALITY
	SYS_STATFS
	SYS_FSTATFS
	SYS_GETPRIORITY
	SYS_SETPRIORITY
	SYS_SCHED_SETPARAM
	SYS_SCHED_GETPARAM
	SYS_SCHED_SETSCHEDULER
	SYS_SCHED_GETSCHEDULER
	SYS_SCHED_GET_PRIORITY_MAX
	SYS_SCHED_GET_PRIORITY_MIN
	SYS_SCHED_RR_GET_INTERVAL
	SYS_MLOCK
	SYS_MUNLOCK
	SYS_MLOCKALL
	SYS_MUNLOCKALL
	SYS_VHANGUP
	SYS_PIVOT_ROOT
	SYS_PRCTL
	SYS_ADJTIMEX
	SYS_SETRLIMIT
	SYS_CHROOT
	SYS_SYNC
	SYS_ACCT
	SYS_SETTIMEOFDAY
	SYS_MOUNT
	SYS_UMOUNT2
	SYS_SWAPON
	SYS_SWAPOFF
	SYS_REBOOT
	SYS_SETHOSTNAME
	SYS_SETDOMAINNAME
	SYS_INIT_MODULE
	SYS_DELETE_MODULE
	SYS_QUOTACTL
	SYS_NFSSERVCTL
	SYS_GETTID
	SYS_READAHEAD
	SYS_SETXATTR
	SYS_LSETXATTR
	SYS_FSETXATTR
	SYS_GETXATTR
	SYS_LGETXATTR
	SYS_FGETXATTR
	SYS_LISTXATTR
	SYS_LLISTXATTR
	SYS_FLISTXATTR
	SYS_REMOVEXATTR
	SYS_LREMOVEXATTR
	SYS_FREMOVEXATTR
	SYS_TKILL
	SYS_FUTEX
	SYS_SCHED_SETAFFINITY
	SYS_SCHED_GETAFFINITY
	SYS_IO_SETUP
	SYS_IO_DESTROY
	SYS_IO_GETEVENTS
	SYS_IO_SUBMIT
	SYS_IO_CANCEL
	SYS_LOOKUP_DCOOKIE
	SYS_REMAP_FILE_PAGES
	SYS_GETDENTS64
	SYS_SET_TID_ADDRESS
	SYS_RESTART_SYSCALL
	SYS_SEMTIMEDOP
	SYS_FADVISE64
	SYS_TIMER_CREATE
	SYS_TIMER_SETTIME
	SYS_TIMER_GETTIME
	SYS_TIMER_GETOVERRUN
	SYS_TIMER_DELETE
	SYS_CLOCK_SETTIME
	SYS_CLOCK_GETTIME
	SYS_CLOCK_GETRES
	SYS_CLOCK_NANOSLEEP
	SYS_EXIT_GROUP
	SYS_EPOLL_CTL
	SYS_TGKILL
	SYS_MBIND
	SYS_SET_MEMPOLICY
	SYS_GET_MEMPOLICY
	SYS_MQ_OPEN
	SYS_MQ_UNLINK
	SYS_MQ_TIMEDSEND
	SYS_MQ_TIMEDRECEIVE
	SYS_MQ_NOTIFY
	SYS_MQ_GETSETATTR
	SYS_KEXEC_LOAD
	SYS_WAITID
	SYS_ADD_KEY
	SYS_REQUEST_KEY
	SYS_KEYCTL
	SYS_IOPRIO_SET
	SYS_IOPRIO_GET
	SYS_INOTIFY_ADD_WATCH
	SYS_INOTIFY_RM_WATCH
	SYS_MIGRATE_PAGES
	SYS_OPENAT
	SYS_MKDIRAT
	SYS_MKNODAT
	SYS_FCHOWNAT
	SYS_NEWFSTATAT
	SYS_UNLINKAT
	SYS_RENAMEAT
	SYS_LINKAT
	SYS_SYMLINKAT
	SYS_READLINKAT
	SYS_FCHMODAT
	SYS_FACCESSAT
	SYS_PSELECT6
	SYS_PPOLL
	SYS_UNSHARE
	SYS_SET_ROBUST_LIST
	SYS_GET_ROBUST_LIST
	SYS_SPLICE
	SYS_TEE
	SYS_SYNC_FILE_RANGE
	SYS_VMSPLICE
	SYS_MOVE_PAGES
	SYS_UTIMENSAT
	SYS_EPOLL_PWAIT
	SYS_TIMERFD_CREATE
	SYS_FALLOCATE
	SYS_TIMERFD_SETTIME
	SYS_TIMERFD_GETTIME
	SYS_ACCEPT4
	SYS_SIGNALFD4
	SYS_EVENTFD2
	SYS_EPOLL_CREATE1
	SYS_DUP3
	SYS_PIPE2
	SYS_INOTIFY_INIT1
	SYS_PREADV
	SYS_PWRITEV
	SYS_RT_TGSIGQUEUEINFO
	SYS_PERF_EVENT_OPEN
	SYS_RECVMMSG
	SYS_FANOTIFY_INIT
	SYS_FANOTIFY_MARK
	SYS_PRLIMIT64
	SYS_NAME_TO_HANDLE_AT
	SYS_OPEN_BY_HANDLE_AT
	SYS_CLOCK_ADJTIME
	SYS_SYNCFS
	SYS_SENDMMSG
	SYS_SETNS
	SYS_GETCPU
	SYS_PROCESS_VM_READV
	SYS_PROCESS_VM_WRITEV
	SYS_KCMP
	SYS_FINIT_MODULE
	SYS_SCHED_SETATTR
	SYS_SCHED_GETATTR
	SYS_RENAMEAT2
	SYS_SECCOMP
	SYS_GETRANDOM
	SYS_MEMFD_CREATE
	SYS_KEXEC_FILE_LOAD
	SYS_BPF
	SYS_EXECVEAT
	SYS_USERFAULTFD
	SYS_MEMBARRIER
	SYS_MLOCK2
	SYS_COPY_FILE_RANGE
	SYS_PREADV2
	SYS_PWRITEV2
	SYS_PKEY_MPROTECT
	SYS_PKEY_ALLOC
	SYS_PKEY_FREE
	SYS_STATX
	SYS_IO_PGETEVENTS
	SYS_RSEQ
	SYS_PIDFD_SEND_SIGNAL
	SYS_IO_URING_SETUP
	SYS_IO_URING_ENTER
	SYS_IO_URING_REGISTER
	SYS_OPEN_TREE
	SYS_MOVE_MOUNT
	SYS_FSOPEN
	SYS_FSCONFIG
	SYS_FSMOUNT
	SYS_FSPICK
	SYS_PIDFD_OPEN
	SYS_CLONE3
	SYS_CLOSE_RANGE
	SYS_OPENAT2
	SYS_PIDFD_GETFD
	SYS_FACCESSAT2
	SYS_PROCESS_MADVISE
	SYS_EPOLL_PWAIT2
	SYS_MOUNT_SETATTR
	SYS_QUOTACTL_FD
	SYS_LANDLOCK_CREATE_RULESET
	SYS_LANDLOCK_ADD_RULE
	SYS_LANDLOCK_RESTRICT_SELF
	SYS_MEMFD_SECRET
	SYS_PROCESS_MRELEASE
	SYS_FUTEX_WAITV
	SYS_SET_MEMPOLICY_HOME_NODE
	SYS_CACHESTAT
	SYS_FCHMODAT2
	SYS_MAP_SHADOW_STACK
	SYS_FUTEX_WAKE
	SYS_FUTEX_WAIT
	SYS_FUTEX_REQUEUE
	SYS_STATMOUNT
	SYS_LISTMOUNT
	SYS_LSM_GET_SELF_ATTR
	SYS_LSM_SET_SELF_ATTR
	SYS_LSM_LIST_MODULES
	SYS_MSEAL
	SYS_SETXATTRAT
	SYS_GETXATTRAT
	SYS_LISTXATTRAT
	SYS_REMOVEXATTRAT
	SYS_OPEN_TREE_ATTR

	MaximumSyscall
)

var SyscallNames = map[Syscall]string{
	SYS_READ:                    "read",
	SYS_WRITE:                   "write",
	SYS_CLOSE:                   "close",
	SYS_FSTAT:                   "fstat",
	SYS_LSEEK:                   "lseek",
	SYS_MMAP:                    "mmap",
	SYS_MPROTECT:                "mprotect",
	SYS_MUNMAP:                  "munmap",
	SYS_BRK:                     "brk",
	SYS_RT_SIGACTION:            "rt_sigaction",
	SYS_RT_SIGPROCMASK:          "rt_sigprocmask",
	SYS_RT_SIGRETURN:            "rt_sigreturn",
	SYS_IOCTL:                   "ioctl",
	SYS_PREAD64:                 "pread64",
	SYS_PWRITE64:                "pwrite64",
	SYS_READV:                   "readv",
	SYS_WRITEV:                  "writev",
	SYS_SCHED_YIELD:             "sched_yield",
	SYS_MREMAP:                  "mremap",
	SYS_MSYNC:                   "msync",
	SYS_MINCORE:                 "mincore",
	SYS_MADVISE:                 "madvise",
	SYS_SHMGET:                  "shmget",
	SYS_SHMAT:                   "shmat",
	SYS_SHMCTL:                  "shmctl",
	SYS_MSGGET:                  "msgget",
	SYS_MSGSND:                  "msgsnd",
	SYS_MSGRCV:                  "msgrcv",
	SYS_MSGCTL:                  "msgctl",
	SYS_FCNTL:                   "fcntl",
	SYS_FLOCK:                   "flock",
	SYS_FSYNC:                   "fsync",
	SYS_FDATASYNC:               "fdatasync",
	SYS_TRUNCATE:                "truncate",
	SYS_FTRUNCATE:               "ftruncate",
	SYS_GETCWD:                  "getcwd",
	SYS_CHDIR:                   "chdir",
	SYS_FCHDIR:                  "fchdir",
	SYS_FCHMOD:                  "fchmod",
	SYS_FCHOWN:                  "fchown",
	SYS_UMASK:                   "umask",
	SYS_GETTIMEOFDAY:            "gettimeofday",
	SYS_GETRLIMIT:               "getrlimit",
	SYS_GETRUSAGE:               "getrusage",
	SYS_SYSINFO:                 "sysinfo",
	SYS_TIMES:                   "times",
	SYS_PTRACE:                  "ptrace",
	SYS_GETUID:                  "getuid",
	SYS_SYSLOG:                  "syslog",
	SYS_GETGID:                  "getgid",
	SYS_SETUID:                  "setuid",
	SYS_SETGID:                  "setgid",
	SYS_GETEUID:                 "geteuid",
	SYS_GETEGID:                 "getegid",
	SYS_SETPGID:                 "setpgid",
	SYS_GETPPID:                 "getppid",
	SYS_SETSID:                  "setsid",
	SYS_SETREUID:                "setreuid",
	SYS_SETREGID:                "setregid",
	SYS_GETGROUPS:               "getgroups",
	SYS_SETGROUPS:               "setgroups",
	SYS_SETRESUID:               "setresuid",
	SYS_GETRESUID:               "getresuid",
	SYS_SETRESGID:               "setresgid",
	SYS_GETRESGID:               "getresgid",
	SYS_GETPGID:                 "getpgid",
	SYS_SETFSUID:                "setfsuid",
	SYS_SETFSGID:                "setfsgid",
	SYS_GETSID:                  "getsid",
	SYS_CAPGET:                  "capget",
	SYS_CAPSET:                  "capset",
	SYS_RT_SIGPENDING:           "rt_sigpending",
	SYS_RT_SIGTIMEDWAIT:         "rt_sigtimedwait",
	SYS_RT_SIGQUEUEINFO:         "rt_sigqueueinfo",
	SYS_RT_SIGSUSPEND:           "rt_sigsuspend",
	SYS_SIGALTSTACK:             "sigaltstack",
	SYS_PERSONALITY:             "personality",
	SYS_STATFS:                  "statfs",
	SYS_FSTATFS:                 "fstatfs",
	SYS_GETPRIORITY:             "getpriority",
	SYS_SETPRIORITY:             "setpriority",
	SYS_SCHED_SETPARAM:          "sched_setparam",
	SYS_SCHED_GETPARAM:          "sched_getparam",
	SYS_SCHED_SETSCHEDULER:      "sched_setscheduler",
	SYS_SCHED_GETSCHEDULER:      "sched_getscheduler",
	SYS_SCHED_GET_PRIORITY_MAX:  "sched_get_priority_max",
	SYS_SCHED_GET_PRIORITY_MIN:  "sched_get_priority_min",
	SYS_SCHED_RR_GET_INTERVAL:   "sched_rr_get_interval",
	SYS_MLOCK:                   "mlock",
	SYS_MUNLOCK:                 "munlock",
	SYS_MLOCKALL:                "mlockall",
	SYS_MUNLOCKALL:              "munlockall",
	SYS_VHANGUP:                 "vhangup",
	SYS_PIVOT_ROOT:              "pivot_root",
	SYS_PRCTL:                   "prctl",
	SYS_ADJTIMEX:                "adjtimex",
	SYS_SETRLIMIT:               "setrlimit",
	SYS_CHROOT:                  "chroot",
	SYS_SYNC:                    "sync",
	SYS_ACCT:                    "acct",
	SYS_SETTIMEOFDAY:            "settimeofday",
	SYS_MOUNT:                   "mount",
	SYS_UMOUNT2:                 "umount2",
	SYS_SWAPON:                  "swapon",
	SYS_SWAPOFF:                 "swapoff",
	SYS_REBOOT:                  "reboot",
	SYS_SETHOSTNAME:             "sethostname",
	SYS_SETDOMAINNAME:           "setdomainname",
	SYS_INIT_MODULE:             "init_module",
	SYS_DELETE_MODULE:           "delete_module",
	SYS_QUOTACTL:                "quotactl",
	SYS_NFSSERVCTL:              "nfsservctl",
	SYS_GETTID:                  "gettid",
	SYS_READAHEAD:               "readahead",
	SYS_SETXATTR:                "setxattr",
	SYS_LSETXATTR:               "lsetxattr",
	SYS_FSETXATTR:               "fsetxattr",
	SYS_GETXATTR:                "getxattr",
	SYS_LGETXATTR:               "lgetxattr",
	SYS_FGETXATTR:               "fgetxattr",
	SYS_LISTXATTR:               "listxattr",
	SYS_LLISTXATTR:              "llistxattr",
	SYS_FLISTXATTR:              "flistxattr",
	SYS_REMOVEXATTR:             "removexattr",
	SYS_LREMOVEXATTR:            "lremovexattr",
	SYS_FREMOVEXATTR:            "fremovexattr",
	SYS_TKILL:                   "tkill",
	SYS_FUTEX:                   "futex",
	SYS_SCHED_SETAFFINITY:       "sched_setaffinity",
	SYS_SCHED_GETAFFINITY:       "sched_getaffinity",
	SYS_IO_SETUP:                "io_setup",
	SYS_IO_DESTROY:              "io_destroy",
	SYS_IO_GETEVENTS:            "io_getevents",
	SYS_IO_SUBMIT:               "io_submit",
	SYS_IO_CANCEL:               "io_cancel",
	SYS_LOOKUP_DCOOKIE:          "lookup_dcookie",
	SYS_REMAP_FILE_PAGES:        "remap_file_pages",
	SYS_GETDENTS64:              "getdents64",
	SYS_SET_TID_ADDRESS:         "set_tid_address",
	SYS_RESTART_SYSCALL:         "restart_syscall",
	SYS_SEMTIMEDOP:              "semtimedop",
	SYS_FADVISE64:               "fadvise64",
	SYS_TIMER_CREATE:            "timer_create",
	SYS_TIMER_SETTIME:           "timer_settime",
	SYS_TIMER_GETTIME:           "timer_gettime",
	SYS_TIMER_GETOVERRUN:        "timer_getoverrun",
	SYS_TIMER_DELETE:            "timer_delete",
	SYS_CLOCK_SETTIME:           "clock_settime",
	SYS_CLOCK_GETTIME:           "clock_gettime",
	SYS_CLOCK_GETRES:            "clock_getres",
	SYS_CLOCK_NANOSLEEP:         "clock_nanosleep",
	SYS_EXIT_GROUP:              "exit_group",
	SYS_EPOLL_CTL:               "epoll_ctl",
	SYS_TGKILL:                  "tgkill",
	SYS_MBIND:                   "mbind",
	SYS_SET_MEMPOLICY:           "set_mempolicy",
	SYS_GET_MEMPOLICY:           "get_mempolicy",
	SYS_MQ_OPEN:                 "mq_open",
	SYS_MQ_UNLINK:               "mq_unlink",
	SYS_MQ_TIMEDSEND:            "mq_timedsend",
	SYS_MQ_TIMEDRECEIVE:         "mq_timedreceive",
	SYS_MQ_NOTIFY:               "mq_notify",
	SYS_MQ_GETSETATTR:           "mq_getsetattr",
	SYS_KEXEC_LOAD:              "kexec_load",
	SYS_WAITID:                  "waitid",
	SYS_ADD_KEY:                 "add_key",
	SYS_REQUEST_KEY:             "request_key",
	SYS_KEYCTL:                  "keyctl",
	SYS_IOPRIO_SET:              "ioprio_set",
	SYS_IOPRIO_GET:              "ioprio_get",
	SYS_INOTIFY_ADD_WATCH:       "inotify_add_watch",
	SYS_INOTIFY_RM_WATCH:        "inotify_rm_watch",
	SYS_MIGRATE_PAGES:           "migrate_pages",
	SYS_OPENAT:                  "openat",
	SYS_MKDIRAT:                 "mkdirat",
	SYS_MKNODAT:                 "mknodat",
	SYS_FCHOWNAT:                "fchownat",
	SYS_NEWFSTATAT:              "newfstatat",
	SYS_UNLINKAT:                "unlinkat",
	SYS_RENAMEAT:                "renameat",
	SYS_LINKAT:                  "linkat",
	SYS_SYMLINKAT:               "symlinkat",
	SYS_READLINKAT:              "readlinkat",
	SYS_FCHMODAT:                "fchmodat",
	SYS_FACCESSAT:               "faccessat",
	SYS_PSELECT6:                "pselect6",
	SYS_PPOLL:                   "ppoll",
	SYS_UNSHARE:                 "unshare",
	SYS_SET_ROBUST_LIST:         "set_robust_list",
	SYS_GET_ROBUST_LIST:         "get_robust_list",
	SYS_SPLICE:                  "splice",
	SYS_TEE:                     "tee",
	SYS_SYNC_FILE_RANGE:         "sync_file_range",
	SYS_VMSPLICE:                "vmsplice",
	SYS_MOVE_PAGES:              "move_pages",
	SYS_UTIMENSAT:               "utimensat",
	SYS_EPOLL_PWAIT:             "epoll_pwait",
	SYS_TIMERFD_CREATE:          "timerfd_create",
	SYS_FALLOCATE:               "fallocate",
	SYS_TIMERFD_SETTIME:         "timerfd_settime",
	SYS_TIMERFD_GETTIME:         "timerfd_gettime",
	SYS_ACCEPT4:                 "accept4",
	SYS_SIGNALFD4:               "signalfd4",
	SYS_EVENTFD2:                "eventfd2",
	SYS_EPOLL_CREATE1:           "epoll_create1",
	SYS_DUP3:                    "dup3",
	SYS_PIPE2:                   "pipe2",
	SYS_INOTIFY_INIT1:           "inotify_init1",
	SYS_PREADV:                  "preadv",
	SYS_PWRITEV:                 "pwritev",
	SYS_RT_TGSIGQUEUEINFO:       "rt_tgsigqueueinfo",
	SYS_PERF_EVENT_OPEN:         "perf_event_open",
	SYS_RECVMMSG:                "recvmmsg",
	SYS_FANOTIFY_INIT:           "fanotify_init",
	SYS_FANOTIFY_MARK:           "fanotify_mark",
	SYS_PRLIMIT64:               "prlimit64",
	SYS_NAME_TO_HANDLE_AT:       "name_to_handle_at",
	SYS_OPEN_BY_HANDLE_AT:       "open_by_handle_at",
	SYS_CLOCK_ADJTIME:           "clock_adjtime",
	SYS_SYNCFS:                  "syncfs",
	SYS_SENDMMSG:                "sendmmsg",
	SYS_SETNS:                   "setns",
	SYS_GETCPU:                  "getcpu",
	SYS_PROCESS_VM_READV:        "process_vm_readv",
	SYS_PROCESS_VM_WRITEV:       "process_vm_writev",
	SYS_KCMP:                    "kcmp",
	SYS_FINIT_MODULE:            "finit_module",
	SYS_SCHED_SETATTR:           "sched_setattr",
	SYS_SCHED_GETATTR:           "sched_getattr",
	SYS_RENAMEAT2:               "renameat2",
	SYS_SECCOMP:                 "seccomp",
	SYS_GETRANDOM:               "getrandom",
	SYS_MEMFD_CREATE:            "memfd_create",
	SYS_KEXEC_FILE_LOAD:         "kexec_file_load",
	SYS_BPF:                     "bpf",
	SYS_EXECVEAT:                "execveat",
	SYS_USERFAULTFD:             "userfaultfd",
	SYS_MEMBARRIER:              "membarrier",
	SYS_MLOCK2:                  "mlock2",
	SYS_COPY_FILE_RANGE:         "copy_file_range",
	SYS_PREADV2:                 "preadv2",
	SYS_PWRITEV2:                "pwritev2",
	SYS_PKEY_MPROTECT:           "pkey_mprotect",
	SYS_PKEY_ALLOC:              "pkey_alloc",
	SYS_PKEY_FREE:               "pkey_free",
	SYS_STATX:                   "statx",
	SYS_IO_PGETEVENTS:           "io_pgetevents",
	SYS_RSEQ:                    "rseq",
	SYS_PIDFD_SEND_SIGNAL:       "pidfd_send_signal",
	SYS_IO_URING_SETUP:          "io_uring_setup",
	SYS_IO_URING_ENTER:          "io_uring_enter",
	SYS_IO_URING_REGISTER:       "io_uring_register",
	SYS_OPEN_TREE:               "open_tree",
	SYS_MOVE_MOUNT:              "move_mount",
	SYS_FSOPEN:                  "fsopen",
	SYS_FSCONFIG:                "fsconfig",
	SYS_FSMOUNT:                 "fsmount",
	SYS_FSPICK:                  "fspick",
	SYS_PIDFD_OPEN:              "pidfd_open",
	SYS_CLONE3:                  "clone3",
	SYS_CLOSE_RANGE:             "close_range",
	SYS_OPENAT2:                 "openat2",
	SYS_PIDFD_GETFD:             "pidfd_getfd",
	SYS_FACCESSAT2:              "faccessat2",
	SYS_PROCESS_MADVISE:         "process_madvise",
	SYS_EPOLL_PWAIT2:            "epoll_pwait2",
	SYS_MOUNT_SETATTR:           "mount_setattr",
	SYS_QUOTACTL_FD:             "quotactl_fd",
	SYS_LANDLOCK_CREATE_RULESET: "landlock_create_ruleset",
	SYS_LANDLOCK_ADD_RULE:       "landlock_add_rule",
	SYS_LANDLOCK_RESTRICT_SELF:  "landlock_restrict_self",
	SYS_MEMFD_SECRET:            "memfd_secret",
	SYS_PROCESS_MRELEASE:        "process_mrelease",
	SYS_FUTEX_WAITV:             "futex_waitv",
	SYS_SET_MEMPOLICY_HOME_NODE: "set_mempolicy_home_node",
	SYS_CACHESTAT:               "cachestat",
	SYS_FCHMODAT2:               "fchmodat2",
	SYS_MAP_SHADOW_STACK:        "map_shadow_stack",
}

func (s Syscall) String() string {
	if name, ok := SyscallNames[s]; ok {
		return name
	}
	return fmt.Sprintf("syscall#%d", s)
}

type Signal int

var (
	SIGCHLD Signal = Signal(0x11)
)
