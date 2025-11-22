//go:build amd64

// Just all the defs from golang.org/x/sys/unix for linux/amd64

package linux

import "strconv"

const (
	SYS_READ                    = 0
	SYS_WRITE                   = 1
	SYS_OPEN                    = 2
	SYS_CLOSE                   = 3
	SYS_STAT                    = 4
	SYS_FSTAT                   = 5
	SYS_LSTAT                   = 6
	SYS_POLL                    = 7
	SYS_LSEEK                   = 8
	SYS_MMAP                    = 9
	SYS_MPROTECT                = 10
	SYS_MUNMAP                  = 11
	SYS_BRK                     = 12
	SYS_RT_SIGACTION            = 13
	SYS_RT_SIGPROCMASK          = 14
	SYS_RT_SIGRETURN            = 15
	SYS_IOCTL                   = 16
	SYS_PREAD64                 = 17
	SYS_PWRITE64                = 18
	SYS_READV                   = 19
	SYS_WRITEV                  = 20
	SYS_ACCESS                  = 21
	SYS_PIPE                    = 22
	SYS_SELECT                  = 23
	SYS_SCHED_YIELD             = 24
	SYS_MREMAP                  = 25
	SYS_MSYNC                   = 26
	SYS_MINCORE                 = 27
	SYS_MADVISE                 = 28
	SYS_SHMGET                  = 29
	SYS_SHMAT                   = 30
	SYS_SHMCTL                  = 31
	SYS_DUP                     = 32
	SYS_DUP2                    = 33
	SYS_PAUSE                   = 34
	SYS_NANOSLEEP               = 35
	SYS_GETITIMER               = 36
	SYS_ALARM                   = 37
	SYS_SETITIMER               = 38
	SYS_GETPID                  = 39
	SYS_SENDFILE                = 40
	SYS_SOCKET                  = 41
	SYS_CONNECT                 = 42
	SYS_ACCEPT                  = 43
	SYS_SENDTO                  = 44
	SYS_RECVFROM                = 45
	SYS_SENDMSG                 = 46
	SYS_RECVMSG                 = 47
	SYS_SHUTDOWN                = 48
	SYS_BIND                    = 49
	SYS_LISTEN                  = 50
	SYS_GETSOCKNAME             = 51
	SYS_GETPEERNAME             = 52
	SYS_SOCKETPAIR              = 53
	SYS_SETSOCKOPT              = 54
	SYS_GETSOCKOPT              = 55
	SYS_CLONE                   = 56
	SYS_FORK                    = 57
	SYS_VFORK                   = 58
	SYS_EXECVE                  = 59
	SYS_EXIT                    = 60
	SYS_WAIT4                   = 61
	SYS_KILL                    = 62
	SYS_UNAME                   = 63
	SYS_SEMGET                  = 64
	SYS_SEMOP                   = 65
	SYS_SEMCTL                  = 66
	SYS_SHMDT                   = 67
	SYS_MSGGET                  = 68
	SYS_MSGSND                  = 69
	SYS_MSGRCV                  = 70
	SYS_MSGCTL                  = 71
	SYS_FCNTL                   = 72
	SYS_FLOCK                   = 73
	SYS_FSYNC                   = 74
	SYS_FDATASYNC               = 75
	SYS_TRUNCATE                = 76
	SYS_FTRUNCATE               = 77
	SYS_GETDENTS                = 78
	SYS_GETCWD                  = 79
	SYS_CHDIR                   = 80
	SYS_FCHDIR                  = 81
	SYS_RENAME                  = 82
	SYS_MKDIR                   = 83
	SYS_RMDIR                   = 84
	SYS_CREAT                   = 85
	SYS_LINK                    = 86
	SYS_UNLINK                  = 87
	SYS_SYMLINK                 = 88
	SYS_READLINK                = 89
	SYS_CHMOD                   = 90
	SYS_FCHMOD                  = 91
	SYS_CHOWN                   = 92
	SYS_FCHOWN                  = 93
	SYS_LCHOWN                  = 94
	SYS_UMASK                   = 95
	SYS_GETTIMEOFDAY            = 96
	SYS_GETRLIMIT               = 97
	SYS_GETRUSAGE               = 98
	SYS_SYSINFO                 = 99
	SYS_TIMES                   = 100
	SYS_PTRACE                  = 101
	SYS_GETUID                  = 102
	SYS_SYSLOG                  = 103
	SYS_GETGID                  = 104
	SYS_SETUID                  = 105
	SYS_SETGID                  = 106
	SYS_GETEUID                 = 107
	SYS_GETEGID                 = 108
	SYS_SETPGID                 = 109
	SYS_GETPPID                 = 110
	SYS_GETPGRP                 = 111
	SYS_SETSID                  = 112
	SYS_SETREUID                = 113
	SYS_SETREGID                = 114
	SYS_GETGROUPS               = 115
	SYS_SETGROUPS               = 116
	SYS_SETRESUID               = 117
	SYS_GETRESUID               = 118
	SYS_SETRESGID               = 119
	SYS_GETRESGID               = 120
	SYS_GETPGID                 = 121
	SYS_SETFSUID                = 122
	SYS_SETFSGID                = 123
	SYS_GETSID                  = 124
	SYS_CAPGET                  = 125
	SYS_CAPSET                  = 126
	SYS_RT_SIGPENDING           = 127
	SYS_RT_SIGTIMEDWAIT         = 128
	SYS_RT_SIGQUEUEINFO         = 129
	SYS_RT_SIGSUSPEND           = 130
	SYS_SIGALTSTACK             = 131
	SYS_UTIME                   = 132
	SYS_MKNOD                   = 133
	SYS_USELIB                  = 134
	SYS_PERSONALITY             = 135
	SYS_USTAT                   = 136
	SYS_STATFS                  = 137
	SYS_FSTATFS                 = 138
	SYS_SYSFS                   = 139
	SYS_GETPRIORITY             = 140
	SYS_SETPRIORITY             = 141
	SYS_SCHED_SETPARAM          = 142
	SYS_SCHED_GETPARAM          = 143
	SYS_SCHED_SETSCHEDULER      = 144
	SYS_SCHED_GETSCHEDULER      = 145
	SYS_SCHED_GET_PRIORITY_MAX  = 146
	SYS_SCHED_GET_PRIORITY_MIN  = 147
	SYS_SCHED_RR_GET_INTERVAL   = 148
	SYS_MLOCK                   = 149
	SYS_MUNLOCK                 = 150
	SYS_MLOCKALL                = 151
	SYS_MUNLOCKALL              = 152
	SYS_VHANGUP                 = 153
	SYS_MODIFY_LDT              = 154
	SYS_PIVOT_ROOT              = 155
	SYS__SYSCTL                 = 156
	SYS_PRCTL                   = 157
	SYS_ARCH_PRCTL              = 158
	SYS_ADJTIMEX                = 159
	SYS_SETRLIMIT               = 160
	SYS_CHROOT                  = 161
	SYS_SYNC                    = 162
	SYS_ACCT                    = 163
	SYS_SETTIMEOFDAY            = 164
	SYS_MOUNT                   = 165
	SYS_UMOUNT2                 = 166
	SYS_SWAPON                  = 167
	SYS_SWAPOFF                 = 168
	SYS_REBOOT                  = 169
	SYS_SETHOSTNAME             = 170
	SYS_SETDOMAINNAME           = 171
	SYS_IOPL                    = 172
	SYS_IOPERM                  = 173
	SYS_CREATE_MODULE           = 174
	SYS_INIT_MODULE             = 175
	SYS_DELETE_MODULE           = 176
	SYS_GET_KERNEL_SYMS         = 177
	SYS_QUERY_MODULE            = 178
	SYS_QUOTACTL                = 179
	SYS_NFSSERVCTL              = 180
	SYS_GETPMSG                 = 181
	SYS_PUTPMSG                 = 182
	SYS_AFS_SYSCALL             = 183
	SYS_TUXCALL                 = 184
	SYS_SECURITY                = 185
	SYS_GETTID                  = 186
	SYS_READAHEAD               = 187
	SYS_SETXATTR                = 188
	SYS_LSETXATTR               = 189
	SYS_FSETXATTR               = 190
	SYS_GETXATTR                = 191
	SYS_LGETXATTR               = 192
	SYS_FGETXATTR               = 193
	SYS_LISTXATTR               = 194
	SYS_LLISTXATTR              = 195
	SYS_FLISTXATTR              = 196
	SYS_REMOVEXATTR             = 197
	SYS_LREMOVEXATTR            = 198
	SYS_FREMOVEXATTR            = 199
	SYS_TKILL                   = 200
	SYS_TIME                    = 201
	SYS_FUTEX                   = 202
	SYS_SCHED_SETAFFINITY       = 203
	SYS_SCHED_GETAFFINITY       = 204
	SYS_SET_THREAD_AREA         = 205
	SYS_IO_SETUP                = 206
	SYS_IO_DESTROY              = 207
	SYS_IO_GETEVENTS            = 208
	SYS_IO_SUBMIT               = 209
	SYS_IO_CANCEL               = 210
	SYS_GET_THREAD_AREA         = 211
	SYS_LOOKUP_DCOOKIE          = 212
	SYS_EPOLL_CREATE            = 213
	SYS_EPOLL_CTL_OLD           = 214
	SYS_EPOLL_WAIT_OLD          = 215
	SYS_REMAP_FILE_PAGES        = 216
	SYS_GETDENTS64              = 217
	SYS_SET_TID_ADDRESS         = 218
	SYS_RESTART_SYSCALL         = 219
	SYS_SEMTIMEDOP              = 220
	SYS_FADVISE64               = 221
	SYS_TIMER_CREATE            = 222
	SYS_TIMER_SETTIME           = 223
	SYS_TIMER_GETTIME           = 224
	SYS_TIMER_GETOVERRUN        = 225
	SYS_TIMER_DELETE            = 226
	SYS_CLOCK_SETTIME           = 227
	SYS_CLOCK_GETTIME           = 228
	SYS_CLOCK_GETRES            = 229
	SYS_CLOCK_NANOSLEEP         = 230
	SYS_EXIT_GROUP              = 231
	SYS_EPOLL_WAIT              = 232
	SYS_EPOLL_CTL               = 233
	SYS_TGKILL                  = 234
	SYS_UTIMES                  = 235
	SYS_VSERVER                 = 236
	SYS_MBIND                   = 237
	SYS_SET_MEMPOLICY           = 238
	SYS_GET_MEMPOLICY           = 239
	SYS_MQ_OPEN                 = 240
	SYS_MQ_UNLINK               = 241
	SYS_MQ_TIMEDSEND            = 242
	SYS_MQ_TIMEDRECEIVE         = 243
	SYS_MQ_NOTIFY               = 244
	SYS_MQ_GETSETATTR           = 245
	SYS_KEXEC_LOAD              = 246
	SYS_WAITID                  = 247
	SYS_ADD_KEY                 = 248
	SYS_REQUEST_KEY             = 249
	SYS_KEYCTL                  = 250
	SYS_IOPRIO_SET              = 251
	SYS_IOPRIO_GET              = 252
	SYS_INOTIFY_INIT            = 253
	SYS_INOTIFY_ADD_WATCH       = 254
	SYS_INOTIFY_RM_WATCH        = 255
	SYS_MIGRATE_PAGES           = 256
	SYS_OPENAT                  = 257
	SYS_MKDIRAT                 = 258
	SYS_MKNODAT                 = 259
	SYS_FCHOWNAT                = 260
	SYS_FUTIMESAT               = 261
	SYS_NEWFSTATAT              = 262
	SYS_UNLINKAT                = 263
	SYS_RENAMEAT                = 264
	SYS_LINKAT                  = 265
	SYS_SYMLINKAT               = 266
	SYS_READLINKAT              = 267
	SYS_FCHMODAT                = 268
	SYS_FACCESSAT               = 269
	SYS_PSELECT6                = 270
	SYS_PPOLL                   = 271
	SYS_UNSHARE                 = 272
	SYS_SET_ROBUST_LIST         = 273
	SYS_GET_ROBUST_LIST         = 274
	SYS_SPLICE                  = 275
	SYS_TEE                     = 276
	SYS_SYNC_FILE_RANGE         = 277
	SYS_VMSPLICE                = 278
	SYS_MOVE_PAGES              = 279
	SYS_UTIMENSAT               = 280
	SYS_EPOLL_PWAIT             = 281
	SYS_SIGNALFD                = 282
	SYS_TIMERFD_CREATE          = 283
	SYS_EVENTFD                 = 284
	SYS_FALLOCATE               = 285
	SYS_TIMERFD_SETTIME         = 286
	SYS_TIMERFD_GETTIME         = 287
	SYS_ACCEPT4                 = 288
	SYS_SIGNALFD4               = 289
	SYS_EVENTFD2                = 290
	SYS_EPOLL_CREATE1           = 291
	SYS_DUP3                    = 292
	SYS_PIPE2                   = 293
	SYS_INOTIFY_INIT1           = 294
	SYS_PREADV                  = 295
	SYS_PWRITEV                 = 296
	SYS_RT_TGSIGQUEUEINFO       = 297
	SYS_PERF_EVENT_OPEN         = 298
	SYS_RECVMMSG                = 299
	SYS_FANOTIFY_INIT           = 300
	SYS_FANOTIFY_MARK           = 301
	SYS_PRLIMIT64               = 302
	SYS_NAME_TO_HANDLE_AT       = 303
	SYS_OPEN_BY_HANDLE_AT       = 304
	SYS_CLOCK_ADJTIME           = 305
	SYS_SYNCFS                  = 306
	SYS_SENDMMSG                = 307
	SYS_SETNS                   = 308
	SYS_GETCPU                  = 309
	SYS_PROCESS_VM_READV        = 310
	SYS_PROCESS_VM_WRITEV       = 311
	SYS_KCMP                    = 312
	SYS_FINIT_MODULE            = 313
	SYS_SCHED_SETATTR           = 314
	SYS_SCHED_GETATTR           = 315
	SYS_RENAMEAT2               = 316
	SYS_SECCOMP                 = 317
	SYS_GETRANDOM               = 318
	SYS_MEMFD_CREATE            = 319
	SYS_KEXEC_FILE_LOAD         = 320
	SYS_BPF                     = 321
	SYS_EXECVEAT                = 322
	SYS_USERFAULTFD             = 323
	SYS_MEMBARRIER              = 324
	SYS_MLOCK2                  = 325
	SYS_COPY_FILE_RANGE         = 326
	SYS_PREADV2                 = 327
	SYS_PWRITEV2                = 328
	SYS_PKEY_MPROTECT           = 329
	SYS_PKEY_ALLOC              = 330
	SYS_PKEY_FREE               = 331
	SYS_STATX                   = 332
	SYS_IO_PGETEVENTS           = 333
	SYS_RSEQ                    = 334
	SYS_URETPROBE               = 335
	SYS_PIDFD_SEND_SIGNAL       = 424
	SYS_IO_URING_SETUP          = 425
	SYS_IO_URING_ENTER          = 426
	SYS_IO_URING_REGISTER       = 427
	SYS_OPEN_TREE               = 428
	SYS_MOVE_MOUNT              = 429
	SYS_FSOPEN                  = 430
	SYS_FSCONFIG                = 431
	SYS_FSMOUNT                 = 432
	SYS_FSPICK                  = 433
	SYS_PIDFD_OPEN              = 434
	SYS_CLONE3                  = 435
	SYS_CLOSE_RANGE             = 436
	SYS_OPENAT2                 = 437
	SYS_PIDFD_GETFD             = 438
	SYS_FACCESSAT2              = 439
	SYS_PROCESS_MADVISE         = 440
	SYS_EPOLL_PWAIT2            = 441
	SYS_MOUNT_SETATTR           = 442
	SYS_QUOTACTL_FD             = 443
	SYS_LANDLOCK_CREATE_RULESET = 444
	SYS_LANDLOCK_ADD_RULE       = 445
	SYS_LANDLOCK_RESTRICT_SELF  = 446
	SYS_MEMFD_SECRET            = 447
	SYS_PROCESS_MRELEASE        = 448
	SYS_FUTEX_WAITV             = 449
	SYS_SET_MEMPOLICY_HOME_NODE = 450
	SYS_CACHESTAT               = 451
	SYS_FCHMODAT2               = 452
	SYS_MAP_SHADOW_STACK        = 453
	SYS_FUTEX_WAKE              = 454
	SYS_FUTEX_WAIT              = 455
	SYS_FUTEX_REQUEUE           = 456
	SYS_STATMOUNT               = 457
	SYS_LISTMOUNT               = 458
	SYS_LSM_GET_SELF_ATTR       = 459
	SYS_LSM_SET_SELF_ATTR       = 460
	SYS_LSM_LIST_MODULES        = 461
	SYS_MSEAL                   = 462
	SYS_SETXATTRAT              = 463
	SYS_GETXATTRAT              = 464
	SYS_LISTXATTRAT             = 465
	SYS_REMOVEXATTRAT           = 466
	SYS_OPEN_TREE_ATTR          = 467
)

const (
	AAFS_MAGIC                                  = 0x5a3c69f0
	ADFS_SUPER_MAGIC                            = 0xadf5
	AFFS_SUPER_MAGIC                            = 0xadff
	AFS_FS_MAGIC                                = 0x6b414653
	AFS_SUPER_MAGIC                             = 0x5346414f
	AF_ALG                                      = 0x26
	AF_APPLETALK                                = 0x5
	AF_ASH                                      = 0x12
	AF_ATMPVC                                   = 0x8
	AF_ATMSVC                                   = 0x14
	AF_AX25                                     = 0x3
	AF_BLUETOOTH                                = 0x1f
	AF_BRIDGE                                   = 0x7
	AF_CAIF                                     = 0x25
	AF_CAN                                      = 0x1d
	AF_DECnet                                   = 0xc
	AF_ECONET                                   = 0x13
	AF_FILE                                     = 0x1
	AF_IB                                       = 0x1b
	AF_IEEE802154                               = 0x24
	AF_INET                                     = 0x2
	AF_INET6                                    = 0xa
	AF_IPX                                      = 0x4
	AF_IRDA                                     = 0x17
	AF_ISDN                                     = 0x22
	AF_IUCV                                     = 0x20
	AF_KCM                                      = 0x29
	AF_KEY                                      = 0xf
	AF_LLC                                      = 0x1a
	AF_LOCAL                                    = 0x1
	AF_MAX                                      = 0x2e
	AF_MCTP                                     = 0x2d
	AF_MPLS                                     = 0x1c
	AF_NETBEUI                                  = 0xd
	AF_NETLINK                                  = 0x10
	AF_NETROM                                   = 0x6
	AF_NFC                                      = 0x27
	AF_PACKET                                   = 0x11
	AF_PHONET                                   = 0x23
	AF_PPPOX                                    = 0x18
	AF_QIPCRTR                                  = 0x2a
	AF_RDS                                      = 0x15
	AF_ROSE                                     = 0xb
	AF_ROUTE                                    = 0x10
	AF_RXRPC                                    = 0x21
	AF_SECURITY                                 = 0xe
	AF_SMC                                      = 0x2b
	AF_SNA                                      = 0x16
	AF_TIPC                                     = 0x1e
	AF_UNIX                                     = 0x1
	AF_UNSPEC                                   = 0x0
	AF_VSOCK                                    = 0x28
	AF_WANPIPE                                  = 0x19
	AF_X25                                      = 0x9
	AF_XDP                                      = 0x2c
	ALG_OP_DECRYPT                              = 0x0
	ALG_OP_ENCRYPT                              = 0x1
	ALG_SET_AEAD_ASSOCLEN                       = 0x4
	ALG_SET_AEAD_AUTHSIZE                       = 0x5
	ALG_SET_DRBG_ENTROPY                        = 0x6
	ALG_SET_IV                                  = 0x2
	ALG_SET_KEY                                 = 0x1
	ALG_SET_KEY_BY_KEY_SERIAL                   = 0x7
	ALG_SET_OP                                  = 0x3
	ANON_INODE_FS_MAGIC                         = 0x9041934
	ARPHRD_6LOWPAN                              = 0x339
	ARPHRD_ADAPT                                = 0x108
	ARPHRD_APPLETLK                             = 0x8
	ARPHRD_ARCNET                               = 0x7
	ARPHRD_ASH                                  = 0x30d
	ARPHRD_ATM                                  = 0x13
	ARPHRD_AX25                                 = 0x3
	ARPHRD_BIF                                  = 0x307
	ARPHRD_CAIF                                 = 0x336
	ARPHRD_CAN                                  = 0x118
	ARPHRD_CHAOS                                = 0x5
	ARPHRD_CISCO                                = 0x201
	ARPHRD_CSLIP                                = 0x101
	ARPHRD_CSLIP6                               = 0x103
	ARPHRD_DDCMP                                = 0x205
	ARPHRD_DLCI                                 = 0xf
	ARPHRD_ECONET                               = 0x30e
	ARPHRD_EETHER                               = 0x2
	ARPHRD_ETHER                                = 0x1
	ARPHRD_EUI64                                = 0x1b
	ARPHRD_FCAL                                 = 0x311
	ARPHRD_FCFABRIC                             = 0x313
	ARPHRD_FCPL                                 = 0x312
	ARPHRD_FCPP                                 = 0x310
	ARPHRD_FDDI                                 = 0x306
	ARPHRD_FRAD                                 = 0x302
	ARPHRD_HDLC                                 = 0x201
	ARPHRD_HIPPI                                = 0x30c
	ARPHRD_HWX25                                = 0x110
	ARPHRD_IEEE1394                             = 0x18
	ARPHRD_IEEE802                              = 0x6
	ARPHRD_IEEE80211                            = 0x321
	ARPHRD_IEEE80211_PRISM                      = 0x322
	ARPHRD_IEEE80211_RADIOTAP                   = 0x323
	ARPHRD_IEEE802154                           = 0x324
	ARPHRD_IEEE802154_MONITOR                   = 0x325
	ARPHRD_IEEE802_TR                           = 0x320
	ARPHRD_INFINIBAND                           = 0x20
	ARPHRD_IP6GRE                               = 0x337
	ARPHRD_IPDDP                                = 0x309
	ARPHRD_IPGRE                                = 0x30a
	ARPHRD_IRDA                                 = 0x30f
	ARPHRD_LAPB                                 = 0x204
	ARPHRD_LOCALTLK                             = 0x305
	ARPHRD_LOOPBACK                             = 0x304
	ARPHRD_MCTP                                 = 0x122
	ARPHRD_METRICOM                             = 0x17
	ARPHRD_NETLINK                              = 0x338
	ARPHRD_NETROM                               = 0x0
	ARPHRD_NONE                                 = 0xfffe
	ARPHRD_PHONET                               = 0x334
	ARPHRD_PHONET_PIPE                          = 0x335
	ARPHRD_PIMREG                               = 0x30b
	ARPHRD_PPP                                  = 0x200
	ARPHRD_PRONET                               = 0x4
	ARPHRD_RAWHDLC                              = 0x206
	ARPHRD_RAWIP                                = 0x207
	ARPHRD_ROSE                                 = 0x10e
	ARPHRD_RSRVD                                = 0x104
	ARPHRD_SIT                                  = 0x308
	ARPHRD_SKIP                                 = 0x303
	ARPHRD_SLIP                                 = 0x100
	ARPHRD_SLIP6                                = 0x102
	ARPHRD_TUNNEL                               = 0x300
	ARPHRD_TUNNEL6                              = 0x301
	ARPHRD_VOID                                 = 0xffff
	ARPHRD_VSOCKMON                             = 0x33a
	ARPHRD_X25                                  = 0x10f
	AUDIT_ADD                                   = 0x3eb
	AUDIT_ADD_RULE                              = 0x3f3
	AUDIT_ALWAYS                                = 0x2
	AUDIT_ANOM_ABEND                            = 0x6a5
	AUDIT_ANOM_CREAT                            = 0x6a7
	AUDIT_ANOM_LINK                             = 0x6a6
	AUDIT_ANOM_PROMISCUOUS                      = 0x6a4
	AUDIT_ARCH                                  = 0xb
	AUDIT_ARCH_AARCH64                          = 0xc00000b7
	AUDIT_ARCH_ALPHA                            = 0xc0009026
	AUDIT_ARCH_ARCOMPACT                        = 0x4000005d
	AUDIT_ARCH_ARCOMPACTBE                      = 0x5d
	AUDIT_ARCH_ARCV2                            = 0x400000c3
	AUDIT_ARCH_ARCV2BE                          = 0xc3
	AUDIT_ARCH_ARM                              = 0x40000028
	AUDIT_ARCH_ARMEB                            = 0x28
	AUDIT_ARCH_C6X                              = 0x4000008c
	AUDIT_ARCH_C6XBE                            = 0x8c
	AUDIT_ARCH_CRIS                             = 0x4000004c
	AUDIT_ARCH_CSKY                             = 0x400000fc
	AUDIT_ARCH_FRV                              = 0x5441
	AUDIT_ARCH_H8300                            = 0x2e
	AUDIT_ARCH_HEXAGON                          = 0xa4
	AUDIT_ARCH_I386                             = 0x40000003
	AUDIT_ARCH_IA64                             = 0xc0000032
	AUDIT_ARCH_LOONGARCH32                      = 0x40000102
	AUDIT_ARCH_LOONGARCH64                      = 0xc0000102
	AUDIT_ARCH_M32R                             = 0x58
	AUDIT_ARCH_M68K                             = 0x4
	AUDIT_ARCH_MICROBLAZE                       = 0xbd
	AUDIT_ARCH_MIPS                             = 0x8
	AUDIT_ARCH_MIPS64                           = 0x80000008
	AUDIT_ARCH_MIPS64N32                        = 0xa0000008
	AUDIT_ARCH_MIPSEL                           = 0x40000008
	AUDIT_ARCH_MIPSEL64                         = 0xc0000008
	AUDIT_ARCH_MIPSEL64N32                      = 0xe0000008
	AUDIT_ARCH_NDS32                            = 0x400000a7
	AUDIT_ARCH_NDS32BE                          = 0xa7
	AUDIT_ARCH_NIOS2                            = 0x40000071
	AUDIT_ARCH_OPENRISC                         = 0x5c
	AUDIT_ARCH_PARISC                           = 0xf
	AUDIT_ARCH_PARISC64                         = 0x8000000f
	AUDIT_ARCH_PPC                              = 0x14
	AUDIT_ARCH_PPC64                            = 0x80000015
	AUDIT_ARCH_PPC64LE                          = 0xc0000015
	AUDIT_ARCH_RISCV32                          = 0x400000f3
	AUDIT_ARCH_RISCV64                          = 0xc00000f3
	AUDIT_ARCH_S390                             = 0x16
	AUDIT_ARCH_S390X                            = 0x80000016
	AUDIT_ARCH_SH                               = 0x2a
	AUDIT_ARCH_SH64                             = 0x8000002a
	AUDIT_ARCH_SHEL                             = 0x4000002a
	AUDIT_ARCH_SHEL64                           = 0xc000002a
	AUDIT_ARCH_SPARC                            = 0x2
	AUDIT_ARCH_SPARC64                          = 0x8000002b
	AUDIT_ARCH_TILEGX                           = 0xc00000bf
	AUDIT_ARCH_TILEGX32                         = 0x400000bf
	AUDIT_ARCH_TILEPRO                          = 0x400000bc
	AUDIT_ARCH_UNICORE                          = 0x4000006e
	AUDIT_ARCH_X86_64                           = 0xc000003e
	AUDIT_ARCH_XTENSA                           = 0x5e
	AUDIT_ARG0                                  = 0xc8
	AUDIT_ARG1                                  = 0xc9
	AUDIT_ARG2                                  = 0xca
	AUDIT_ARG3                                  = 0xcb
	AUDIT_AVC                                   = 0x578
	AUDIT_AVC_PATH                              = 0x57a
	AUDIT_BITMASK_SIZE                          = 0x40
	AUDIT_BIT_MASK                              = 0x8000000
	AUDIT_BIT_TEST                              = 0x48000000
	AUDIT_BPF                                   = 0x536
	AUDIT_BPRM_FCAPS                            = 0x529
	AUDIT_CAPSET                                = 0x52a
	AUDIT_CLASS_CHATTR                          = 0x2
	AUDIT_CLASS_CHATTR_32                       = 0x3
	AUDIT_CLASS_DIR_WRITE                       = 0x0
	AUDIT_CLASS_DIR_WRITE_32                    = 0x1
	AUDIT_CLASS_READ                            = 0x4
	AUDIT_CLASS_READ_32                         = 0x5
	AUDIT_CLASS_SIGNAL                          = 0x8
	AUDIT_CLASS_SIGNAL_32                       = 0x9
	AUDIT_CLASS_WRITE                           = 0x6
	AUDIT_CLASS_WRITE_32                        = 0x7
	AUDIT_COMPARE_AUID_TO_EUID                  = 0x10
	AUDIT_COMPARE_AUID_TO_FSUID                 = 0xe
	AUDIT_COMPARE_AUID_TO_OBJ_UID               = 0x5
	AUDIT_COMPARE_AUID_TO_SUID                  = 0xf
	AUDIT_COMPARE_EGID_TO_FSGID                 = 0x17
	AUDIT_COMPARE_EGID_TO_OBJ_GID               = 0x4
	AUDIT_COMPARE_EGID_TO_SGID                  = 0x18
	AUDIT_COMPARE_EUID_TO_FSUID                 = 0x12
	AUDIT_COMPARE_EUID_TO_OBJ_UID               = 0x3
	AUDIT_COMPARE_EUID_TO_SUID                  = 0x11
	AUDIT_COMPARE_FSGID_TO_OBJ_GID              = 0x9
	AUDIT_COMPARE_FSUID_TO_OBJ_UID              = 0x8
	AUDIT_COMPARE_GID_TO_EGID                   = 0x14
	AUDIT_COMPARE_GID_TO_FSGID                  = 0x15
	AUDIT_COMPARE_GID_TO_OBJ_GID                = 0x2
	AUDIT_COMPARE_GID_TO_SGID                   = 0x16
	AUDIT_COMPARE_SGID_TO_FSGID                 = 0x19
	AUDIT_COMPARE_SGID_TO_OBJ_GID               = 0x7
	AUDIT_COMPARE_SUID_TO_FSUID                 = 0x13
	AUDIT_COMPARE_SUID_TO_OBJ_UID               = 0x6
	AUDIT_COMPARE_UID_TO_AUID                   = 0xa
	AUDIT_COMPARE_UID_TO_EUID                   = 0xb
	AUDIT_COMPARE_UID_TO_FSUID                  = 0xc
	AUDIT_COMPARE_UID_TO_OBJ_UID                = 0x1
	AUDIT_COMPARE_UID_TO_SUID                   = 0xd
	AUDIT_CONFIG_CHANGE                         = 0x519
	AUDIT_CWD                                   = 0x51b
	AUDIT_DAEMON_ABORT                          = 0x4b2
	AUDIT_DAEMON_CONFIG                         = 0x4b3
	AUDIT_DAEMON_END                            = 0x4b1
	AUDIT_DAEMON_START                          = 0x4b0
	AUDIT_DEL                                   = 0x3ec
	AUDIT_DEL_RULE                              = 0x3f4
	AUDIT_DEVMAJOR                              = 0x64
	AUDIT_DEVMINOR                              = 0x65
	AUDIT_DIR                                   = 0x6b
	AUDIT_DM_CTRL                               = 0x53a
	AUDIT_DM_EVENT                              = 0x53b
	AUDIT_EGID                                  = 0x6
	AUDIT_EOE                                   = 0x528
	AUDIT_EQUAL                                 = 0x40000000
	AUDIT_EUID                                  = 0x2
	AUDIT_EVENT_LISTENER                        = 0x537
	AUDIT_EXE                                   = 0x70
	AUDIT_EXECVE                                = 0x51d
	AUDIT_EXIT                                  = 0x67
	AUDIT_FAIL_PANIC                            = 0x2
	AUDIT_FAIL_PRINTK                           = 0x1
	AUDIT_FAIL_SILENT                           = 0x0
	AUDIT_FANOTIFY                              = 0x533
	AUDIT_FD_PAIR                               = 0x525
	AUDIT_FEATURE_BITMAP_ALL                    = 0x7f
	AUDIT_FEATURE_BITMAP_BACKLOG_LIMIT          = 0x1
	AUDIT_FEATURE_BITMAP_BACKLOG_WAIT_TIME      = 0x2
	AUDIT_FEATURE_BITMAP_EXCLUDE_EXTEND         = 0x8
	AUDIT_FEATURE_BITMAP_EXECUTABLE_PATH        = 0x4
	AUDIT_FEATURE_BITMAP_FILTER_FS              = 0x40
	AUDIT_FEATURE_BITMAP_LOST_RESET             = 0x20
	AUDIT_FEATURE_BITMAP_SESSIONID_FILTER       = 0x10
	AUDIT_FEATURE_CHANGE                        = 0x530
	AUDIT_FEATURE_LOGINUID_IMMUTABLE            = 0x1
	AUDIT_FEATURE_ONLY_UNSET_LOGINUID           = 0x0
	AUDIT_FEATURE_VERSION                       = 0x1
	AUDIT_FIELD_COMPARE                         = 0x6f
	AUDIT_FILETYPE                              = 0x6c
	AUDIT_FILTERKEY                             = 0xd2
	AUDIT_FILTER_ENTRY                          = 0x2
	AUDIT_FILTER_EXCLUDE                        = 0x5
	AUDIT_FILTER_EXIT                           = 0x4
	AUDIT_FILTER_FS                             = 0x6
	AUDIT_FILTER_PREPEND                        = 0x10
	AUDIT_FILTER_TASK                           = 0x1
	AUDIT_FILTER_TYPE                           = 0x5
	AUDIT_FILTER_URING_EXIT                     = 0x7
	AUDIT_FILTER_USER                           = 0x0
	AUDIT_FILTER_WATCH                          = 0x3
	AUDIT_FIRST_KERN_ANOM_MSG                   = 0x6a4
	AUDIT_FIRST_USER_MSG                        = 0x44c
	AUDIT_FIRST_USER_MSG2                       = 0x834
	AUDIT_FSGID                                 = 0x8
	AUDIT_FSTYPE                                = 0x1a
	AUDIT_FSUID                                 = 0x4
	AUDIT_GET                                   = 0x3e8
	AUDIT_GET_FEATURE                           = 0x3fb
	AUDIT_GID                                   = 0x5
	AUDIT_GREATER_THAN                          = 0x20000000
	AUDIT_GREATER_THAN_OR_EQUAL                 = 0x60000000
	AUDIT_INODE                                 = 0x66
	AUDIT_INTEGRITY_DATA                        = 0x708
	AUDIT_INTEGRITY_EVM_XATTR                   = 0x70e
	AUDIT_INTEGRITY_HASH                        = 0x70b
	AUDIT_INTEGRITY_METADATA                    = 0x709
	AUDIT_INTEGRITY_PCR                         = 0x70c
	AUDIT_INTEGRITY_POLICY_RULE                 = 0x70f
	AUDIT_INTEGRITY_RULE                        = 0x70d
	AUDIT_INTEGRITY_STATUS                      = 0x70a
	AUDIT_INTEGRITY_USERSPACE                   = 0x710
	AUDIT_IPC                                   = 0x517
	AUDIT_IPC_SET_PERM                          = 0x51f
	AUDIT_IPE_ACCESS                            = 0x58c
	AUDIT_IPE_CONFIG_CHANGE                     = 0x58d
	AUDIT_IPE_POLICY_LOAD                       = 0x58e
	AUDIT_KERNEL                                = 0x7d0
	AUDIT_KERNEL_OTHER                          = 0x524
	AUDIT_KERN_MODULE                           = 0x532
	AUDIT_LANDLOCK_ACCESS                       = 0x58f
	AUDIT_LANDLOCK_DOMAIN                       = 0x590
	AUDIT_LAST_FEATURE                          = 0x1
	AUDIT_LAST_KERN_ANOM_MSG                    = 0x707
	AUDIT_LAST_USER_MSG                         = 0x4af
	AUDIT_LAST_USER_MSG2                        = 0xbb7
	AUDIT_LESS_THAN                             = 0x10000000
	AUDIT_LESS_THAN_OR_EQUAL                    = 0x50000000
	AUDIT_LIST                                  = 0x3ea
	AUDIT_LIST_RULES                            = 0x3f5
	AUDIT_LOGIN                                 = 0x3ee
	AUDIT_LOGINUID                              = 0x9
	AUDIT_LOGINUID_SET                          = 0x18
	AUDIT_MAC_CALIPSO_ADD                       = 0x58a
	AUDIT_MAC_CALIPSO_DEL                       = 0x58b
	AUDIT_MAC_CIPSOV4_ADD                       = 0x57f
	AUDIT_MAC_CIPSOV4_DEL                       = 0x580
	AUDIT_MAC_CONFIG_CHANGE                     = 0x57d
	AUDIT_MAC_IPSEC_ADDSA                       = 0x583
	AUDIT_MAC_IPSEC_ADDSPD                      = 0x585
	AUDIT_MAC_IPSEC_DELSA                       = 0x584
	AUDIT_MAC_IPSEC_DELSPD                      = 0x586
	AUDIT_MAC_IPSEC_EVENT                       = 0x587
	AUDIT_MAC_MAP_ADD                           = 0x581
	AUDIT_MAC_MAP_DEL                           = 0x582
	AUDIT_MAC_POLICY_LOAD                       = 0x57b
	AUDIT_MAC_STATUS                            = 0x57c
	AUDIT_MAC_UNLBL_ALLOW                       = 0x57e
	AUDIT_MAC_UNLBL_STCADD                      = 0x588
	AUDIT_MAC_UNLBL_STCDEL                      = 0x589
	AUDIT_MAKE_EQUIV                            = 0x3f7
	AUDIT_MAX_FIELDS                            = 0x40
	AUDIT_MAX_FIELD_COMPARE                     = 0x19
	AUDIT_MAX_KEY_LEN                           = 0x100
	AUDIT_MESSAGE_TEXT_MAX                      = 0x2170
	AUDIT_MMAP                                  = 0x52b
	AUDIT_MQ_GETSETATTR                         = 0x523
	AUDIT_MQ_NOTIFY                             = 0x522
	AUDIT_MQ_OPEN                               = 0x520
	AUDIT_MQ_SENDRECV                           = 0x521
	AUDIT_MSGTYPE                               = 0xc
	AUDIT_NEGATE                                = 0x80000000
	AUDIT_NETFILTER_CFG                         = 0x52d
	AUDIT_NETFILTER_PKT                         = 0x52c
	AUDIT_NEVER                                 = 0x0
	AUDIT_NLGRP_MAX                             = 0x1
	AUDIT_NOT_EQUAL                             = 0x30000000
	AUDIT_NR_FILTERS                            = 0x8
	AUDIT_OBJ_GID                               = 0x6e
	AUDIT_OBJ_LEV_HIGH                          = 0x17
	AUDIT_OBJ_LEV_LOW                           = 0x16
	AUDIT_OBJ_PID                               = 0x526
	AUDIT_OBJ_ROLE                              = 0x14
	AUDIT_OBJ_TYPE                              = 0x15
	AUDIT_OBJ_UID                               = 0x6d
	AUDIT_OBJ_USER                              = 0x13
	AUDIT_OPENAT2                               = 0x539
	AUDIT_OPERATORS                             = 0x78000000
	AUDIT_PATH                                  = 0x516
	AUDIT_PERM                                  = 0x6a
	AUDIT_PERM_ATTR                             = 0x8
	AUDIT_PERM_EXEC                             = 0x1
	AUDIT_PERM_READ                             = 0x4
	AUDIT_PERM_WRITE                            = 0x2
	AUDIT_PERS                                  = 0xa
	AUDIT_PID                                   = 0x0
	AUDIT_POSSIBLE                              = 0x1
	AUDIT_PPID                                  = 0x12
	AUDIT_PROCTITLE                             = 0x52f
	AUDIT_REPLACE                               = 0x531
	AUDIT_SADDR_FAM                             = 0x71
	AUDIT_SECCOMP                               = 0x52e
	AUDIT_SELINUX_ERR                           = 0x579
	AUDIT_SESSIONID                             = 0x19
	AUDIT_SET                                   = 0x3e9
	AUDIT_SET_FEATURE                           = 0x3fa
	AUDIT_SGID                                  = 0x7
	AUDIT_SID_UNSET                             = 0xffffffff
	AUDIT_SIGNAL_INFO                           = 0x3f2
	AUDIT_SOCKADDR                              = 0x51a
	AUDIT_SOCKETCALL                            = 0x518
	AUDIT_STATUS_BACKLOG_LIMIT                  = 0x10
	AUDIT_STATUS_BACKLOG_WAIT_TIME              = 0x20
	AUDIT_STATUS_BACKLOG_WAIT_TIME_ACTUAL       = 0x80
	AUDIT_STATUS_ENABLED                        = 0x1
	AUDIT_STATUS_FAILURE                        = 0x2
	AUDIT_STATUS_LOST                           = 0x40
	AUDIT_STATUS_PID                            = 0x4
	AUDIT_STATUS_RATE_LIMIT                     = 0x8
	AUDIT_SUBJ_CLR                              = 0x11
	AUDIT_SUBJ_ROLE                             = 0xe
	AUDIT_SUBJ_SEN                              = 0x10
	AUDIT_SUBJ_TYPE                             = 0xf
	AUDIT_SUBJ_USER                             = 0xd
	AUDIT_SUCCESS                               = 0x68
	AUDIT_SUID                                  = 0x3
	AUDIT_SYSCALL                               = 0x514
	AUDIT_SYSCALL_CLASSES                       = 0x10
	AUDIT_TIME_ADJNTPVAL                        = 0x535
	AUDIT_TIME_INJOFFSET                        = 0x534
	AUDIT_TRIM                                  = 0x3f6
	AUDIT_TTY                                   = 0x527
	AUDIT_TTY_GET                               = 0x3f8
	AUDIT_TTY_SET                               = 0x3f9
	AUDIT_UID                                   = 0x1
	AUDIT_UID_UNSET                             = 0xffffffff
	AUDIT_UNUSED_BITS                           = 0x7fffc00
	AUDIT_URINGOP                               = 0x538
	AUDIT_USER                                  = 0x3ed
	AUDIT_USER_AVC                              = 0x453
	AUDIT_USER_TTY                              = 0x464
	AUDIT_VERSION_BACKLOG_LIMIT                 = 0x1
	AUDIT_VERSION_BACKLOG_WAIT_TIME             = 0x2
	AUDIT_VERSION_LATEST                        = 0x7f
	AUDIT_WATCH                                 = 0x69
	AUDIT_WATCH_INS                             = 0x3ef
	AUDIT_WATCH_LIST                            = 0x3f1
	AUDIT_WATCH_REM                             = 0x3f0
	AUTOFS_SUPER_MAGIC                          = 0x187
	B0                                          = 0x0
	B110                                        = 0x3
	B1200                                       = 0x9
	B134                                        = 0x4
	B150                                        = 0x5
	B1800                                       = 0xa
	B19200                                      = 0xe
	B200                                        = 0x6
	B2400                                       = 0xb
	B300                                        = 0x7
	B38400                                      = 0xf
	B4800                                       = 0xc
	B50                                         = 0x1
	B600                                        = 0x8
	B75                                         = 0x2
	B9600                                       = 0xd
	BCACHEFS_SUPER_MAGIC                        = 0xca451a4e
	BDEVFS_MAGIC                                = 0x62646576
	BINDERFS_SUPER_MAGIC                        = 0x6c6f6f70
	BINFMTFS_MAGIC                              = 0x42494e4d
	BPF_A                                       = 0x10
	BPF_ABS                                     = 0x20
	BPF_ADD                                     = 0x0
	BPF_ALU                                     = 0x4
	BPF_ALU64                                   = 0x7
	BPF_AND                                     = 0x50
	BPF_ARSH                                    = 0xc0
	BPF_ATOMIC                                  = 0xc0
	BPF_B                                       = 0x10
	BPF_BUILD_ID_SIZE                           = 0x14
	BPF_CALL                                    = 0x80
	BPF_CMPXCHG                                 = 0xf1
	BPF_DIV                                     = 0x30
	BPF_DW                                      = 0x18
	BPF_END                                     = 0xd0
	BPF_EXIT                                    = 0x90
	BPF_FETCH                                   = 0x1
	BPF_FROM_BE                                 = 0x8
	BPF_FROM_LE                                 = 0x0
	BPF_FS_MAGIC                                = 0xcafe4a11
	BPF_F_AFTER                                 = 0x10
	BPF_F_ALLOW_MULTI                           = 0x2
	BPF_F_ALLOW_OVERRIDE                        = 0x1
	BPF_F_ANY_ALIGNMENT                         = 0x2
	BPF_F_BEFORE                                = 0x8
	BPF_F_ID                                    = 0x20
	BPF_F_NETFILTER_IP_DEFRAG                   = 0x1
	BPF_F_PREORDER                              = 0x40
	BPF_F_QUERY_EFFECTIVE                       = 0x1
	BPF_F_REDIRECT_FLAGS                        = 0x19
	BPF_F_REPLACE                               = 0x4
	BPF_F_SLEEPABLE                             = 0x10
	BPF_F_STRICT_ALIGNMENT                      = 0x1
	BPF_F_TEST_REG_INVARIANTS                   = 0x80
	BPF_F_TEST_RND_HI32                         = 0x4
	BPF_F_TEST_RUN_ON_CPU                       = 0x1
	BPF_F_TEST_SKB_CHECKSUM_COMPLETE            = 0x4
	BPF_F_TEST_STATE_FREQ                       = 0x8
	BPF_F_TEST_XDP_LIVE_FRAMES                  = 0x2
	BPF_F_XDP_DEV_BOUND_ONLY                    = 0x40
	BPF_F_XDP_HAS_FRAGS                         = 0x20
	BPF_H                                       = 0x8
	BPF_IMM                                     = 0x0
	BPF_IND                                     = 0x40
	BPF_JA                                      = 0x0
	BPF_JCOND                                   = 0xe0
	BPF_JEQ                                     = 0x10
	BPF_JGE                                     = 0x30
	BPF_JGT                                     = 0x20
	BPF_JLE                                     = 0xb0
	BPF_JLT                                     = 0xa0
	BPF_JMP                                     = 0x5
	BPF_JMP32                                   = 0x6
	BPF_JNE                                     = 0x50
	BPF_JSET                                    = 0x40
	BPF_JSGE                                    = 0x70
	BPF_JSGT                                    = 0x60
	BPF_JSLE                                    = 0xd0
	BPF_JSLT                                    = 0xc0
	BPF_K                                       = 0x0
	BPF_LD                                      = 0x0
	BPF_LDX                                     = 0x1
	BPF_LEN                                     = 0x80
	BPF_LL_OFF                                  = -0x200000
	BPF_LOAD_ACQ                                = 0x100
	BPF_LSH                                     = 0x60
	BPF_MAJOR_VERSION                           = 0x1
	BPF_MAXINSNS                                = 0x1000
	BPF_MEM                                     = 0x60
	BPF_MEMSX                                   = 0x80
	BPF_MEMWORDS                                = 0x10
	BPF_MINOR_VERSION                           = 0x1
	BPF_MISC                                    = 0x7
	BPF_MOD                                     = 0x90
	BPF_MOV                                     = 0xb0
	BPF_MSH                                     = 0xa0
	BPF_MUL                                     = 0x20
	BPF_NEG                                     = 0x80
	BPF_NET_OFF                                 = -0x100000
	BPF_OBJ_NAME_LEN                            = 0x10
	BPF_OR                                      = 0x40
	BPF_PSEUDO_BTF_ID                           = 0x3
	BPF_PSEUDO_CALL                             = 0x1
	BPF_PSEUDO_FUNC                             = 0x4
	BPF_PSEUDO_KFUNC_CALL                       = 0x2
	BPF_PSEUDO_MAP_FD                           = 0x1
	BPF_PSEUDO_MAP_IDX                          = 0x5
	BPF_PSEUDO_MAP_IDX_VALUE                    = 0x6
	BPF_PSEUDO_MAP_VALUE                        = 0x2
	BPF_RET                                     = 0x6
	BPF_RSH                                     = 0x70
	BPF_ST                                      = 0x2
	BPF_STORE_REL                               = 0x110
	BPF_STX                                     = 0x3
	BPF_SUB                                     = 0x10
	BPF_TAG_SIZE                                = 0x8
	BPF_TAX                                     = 0x0
	BPF_TO_BE                                   = 0x8
	BPF_TO_LE                                   = 0x0
	BPF_TXA                                     = 0x80
	BPF_W                                       = 0x0
	BPF_X                                       = 0x8
	BPF_XADD                                    = 0xc0
	BPF_XCHG                                    = 0xe1
	BPF_XOR                                     = 0xa0
	BRKINT                                      = 0x2
	BS0                                         = 0x0
	BTRFS_SUPER_MAGIC                           = 0x9123683e
	BTRFS_TEST_MAGIC                            = 0x73727279
	BUS_BLUETOOTH                               = 0x5
	BUS_HIL                                     = 0x4
	BUS_USB                                     = 0x3
	BUS_VIRTUAL                                 = 0x6
	CAN_BCM                                     = 0x2
	CAN_BUS_OFF_THRESHOLD                       = 0x100
	CAN_CTRLMODE_3_SAMPLES                      = 0x4
	CAN_CTRLMODE_BERR_REPORTING                 = 0x10
	CAN_CTRLMODE_CC_LEN8_DLC                    = 0x100
	CAN_CTRLMODE_FD                             = 0x20
	CAN_CTRLMODE_FD_NON_ISO                     = 0x80
	CAN_CTRLMODE_LISTENONLY                     = 0x2
	CAN_CTRLMODE_LOOPBACK                       = 0x1
	CAN_CTRLMODE_ONE_SHOT                       = 0x8
	CAN_CTRLMODE_PRESUME_ACK                    = 0x40
	CAN_CTRLMODE_TDC_AUTO                       = 0x200
	CAN_CTRLMODE_TDC_MANUAL                     = 0x400
	CAN_EFF_FLAG                                = 0x80000000
	CAN_EFF_ID_BITS                             = 0x1d
	CAN_EFF_MASK                                = 0x1fffffff
	CAN_ERROR_PASSIVE_THRESHOLD                 = 0x80
	CAN_ERROR_WARNING_THRESHOLD                 = 0x60
	CAN_ERR_ACK                                 = 0x20
	CAN_ERR_BUSERROR                            = 0x80
	CAN_ERR_BUSOFF                              = 0x40
	CAN_ERR_CNT                                 = 0x200
	CAN_ERR_CRTL                                = 0x4
	CAN_ERR_CRTL_ACTIVE                         = 0x40
	CAN_ERR_CRTL_RX_OVERFLOW                    = 0x1
	CAN_ERR_CRTL_RX_PASSIVE                     = 0x10
	CAN_ERR_CRTL_RX_WARNING                     = 0x4
	CAN_ERR_CRTL_TX_OVERFLOW                    = 0x2
	CAN_ERR_CRTL_TX_PASSIVE                     = 0x20
	CAN_ERR_CRTL_TX_WARNING                     = 0x8
	CAN_ERR_CRTL_UNSPEC                         = 0x0
	CAN_ERR_DLC                                 = 0x8
	CAN_ERR_FLAG                                = 0x20000000
	CAN_ERR_LOSTARB                             = 0x2
	CAN_ERR_LOSTARB_UNSPEC                      = 0x0
	CAN_ERR_MASK                                = 0x1fffffff
	CAN_ERR_PROT                                = 0x8
	CAN_ERR_PROT_ACTIVE                         = 0x40
	CAN_ERR_PROT_BIT                            = 0x1
	CAN_ERR_PROT_BIT0                           = 0x8
	CAN_ERR_PROT_BIT1                           = 0x10
	CAN_ERR_PROT_FORM                           = 0x2
	CAN_ERR_PROT_LOC_ACK                        = 0x19
	CAN_ERR_PROT_LOC_ACK_DEL                    = 0x1b
	CAN_ERR_PROT_LOC_CRC_DEL                    = 0x18
	CAN_ERR_PROT_LOC_CRC_SEQ                    = 0x8
	CAN_ERR_PROT_LOC_DATA                       = 0xa
	CAN_ERR_PROT_LOC_DLC                        = 0xb
	CAN_ERR_PROT_LOC_EOF                        = 0x1a
	CAN_ERR_PROT_LOC_ID04_00                    = 0xe
	CAN_ERR_PROT_LOC_ID12_05                    = 0xf
	CAN_ERR_PROT_LOC_ID17_13                    = 0x7
	CAN_ERR_PROT_LOC_ID20_18                    = 0x6
	CAN_ERR_PROT_LOC_ID28_21                    = 0x2
	CAN_ERR_PROT_LOC_IDE                        = 0x5
	CAN_ERR_PROT_LOC_INTERM                     = 0x12
	CAN_ERR_PROT_LOC_RES0                       = 0x9
	CAN_ERR_PROT_LOC_RES1                       = 0xd
	CAN_ERR_PROT_LOC_RTR                        = 0xc
	CAN_ERR_PROT_LOC_SOF                        = 0x3
	CAN_ERR_PROT_LOC_SRTR                       = 0x4
	CAN_ERR_PROT_LOC_UNSPEC                     = 0x0
	CAN_ERR_PROT_OVERLOAD                       = 0x20
	CAN_ERR_PROT_STUFF                          = 0x4
	CAN_ERR_PROT_TX                             = 0x80
	CAN_ERR_PROT_UNSPEC                         = 0x0
	CAN_ERR_RESTARTED                           = 0x100
	CAN_ERR_TRX                                 = 0x10
	CAN_ERR_TRX_CANH_NO_WIRE                    = 0x4
	CAN_ERR_TRX_CANH_SHORT_TO_BAT               = 0x5
	CAN_ERR_TRX_CANH_SHORT_TO_GND               = 0x7
	CAN_ERR_TRX_CANH_SHORT_TO_VCC               = 0x6
	CAN_ERR_TRX_CANL_NO_WIRE                    = 0x40
	CAN_ERR_TRX_CANL_SHORT_TO_BAT               = 0x50
	CAN_ERR_TRX_CANL_SHORT_TO_CANH              = 0x80
	CAN_ERR_TRX_CANL_SHORT_TO_GND               = 0x70
	CAN_ERR_TRX_CANL_SHORT_TO_VCC               = 0x60
	CAN_ERR_TRX_UNSPEC                          = 0x0
	CAN_ERR_TX_TIMEOUT                          = 0x1
	CAN_INV_FILTER                              = 0x20000000
	CAN_ISOTP                                   = 0x6
	CAN_J1939                                   = 0x7
	CAN_MAX_DLC                                 = 0x8
	CAN_MAX_DLEN                                = 0x8
	CAN_MAX_RAW_DLC                             = 0xf
	CAN_MCNET                                   = 0x5
	CAN_MTU                                     = 0x10
	CAN_NPROTO                                  = 0x8
	CAN_RAW                                     = 0x1
	CAN_RAW_FILTER_MAX                          = 0x200
	CAN_RAW_XL_VCID_RX_FILTER                   = 0x4
	CAN_RAW_XL_VCID_TX_PASS                     = 0x2
	CAN_RAW_XL_VCID_TX_SET                      = 0x1
	CAN_RTR_FLAG                                = 0x40000000
	CAN_SFF_ID_BITS                             = 0xb
	CAN_SFF_MASK                                = 0x7ff
	CAN_TERMINATION_DISABLED                    = 0x0
	CAN_TP16                                    = 0x3
	CAN_TP20                                    = 0x4
	CAP_AUDIT_CONTROL                           = 0x1e
	CAP_AUDIT_READ                              = 0x25
	CAP_AUDIT_WRITE                             = 0x1d
	CAP_BLOCK_SUSPEND                           = 0x24
	CAP_BPF                                     = 0x27
	CAP_CHECKPOINT_RESTORE                      = 0x28
	CAP_CHOWN                                   = 0x0
	CAP_DAC_OVERRIDE                            = 0x1
	CAP_DAC_READ_SEARCH                         = 0x2
	CAP_FOWNER                                  = 0x3
	CAP_FSETID                                  = 0x4
	CAP_IPC_LOCK                                = 0xe
	CAP_IPC_OWNER                               = 0xf
	CAP_KILL                                    = 0x5
	CAP_LAST_CAP                                = 0x28
	CAP_LEASE                                   = 0x1c
	CAP_LINUX_IMMUTABLE                         = 0x9
	CAP_MAC_ADMIN                               = 0x21
	CAP_MAC_OVERRIDE                            = 0x20
	CAP_MKNOD                                   = 0x1b
	CAP_NET_ADMIN                               = 0xc
	CAP_NET_BIND_SERVICE                        = 0xa
	CAP_NET_BROADCAST                           = 0xb
	CAP_NET_RAW                                 = 0xd
	CAP_PERFMON                                 = 0x26
	CAP_SETFCAP                                 = 0x1f
	CAP_SETGID                                  = 0x6
	CAP_SETPCAP                                 = 0x8
	CAP_SETUID                                  = 0x7
	CAP_SYSLOG                                  = 0x22
	CAP_SYS_ADMIN                               = 0x15
	CAP_SYS_BOOT                                = 0x16
	CAP_SYS_CHROOT                              = 0x12
	CAP_SYS_MODULE                              = 0x10
	CAP_SYS_NICE                                = 0x17
	CAP_SYS_PACCT                               = 0x14
	CAP_SYS_PTRACE                              = 0x13
	CAP_SYS_RAWIO                               = 0x11
	CAP_SYS_RESOURCE                            = 0x18
	CAP_SYS_TIME                                = 0x19
	CAP_SYS_TTY_CONFIG                          = 0x1a
	CAP_WAKE_ALARM                              = 0x23
	CEPH_SUPER_MAGIC                            = 0xc36400
	CFLUSH                                      = 0xf
	CGROUP2_SUPER_MAGIC                         = 0x63677270
	CGROUP_SUPER_MAGIC                          = 0x27e0eb
	CIFS_SUPER_MAGIC                            = 0xff534d42
	CLOCK_BOOTTIME                              = 0x7
	CLOCK_BOOTTIME_ALARM                        = 0x9
	CLOCK_DEFAULT                               = 0x0
	CLOCK_EXT                                   = 0x1
	CLOCK_INT                                   = 0x2
	CLOCK_MONOTONIC                             = 0x1
	CLOCK_MONOTONIC_COARSE                      = 0x6
	CLOCK_MONOTONIC_RAW                         = 0x4
	CLOCK_PROCESS_CPUTIME_ID                    = 0x2
	CLOCK_REALTIME                              = 0x0
	CLOCK_REALTIME_ALARM                        = 0x8
	CLOCK_REALTIME_COARSE                       = 0x5
	CLOCK_TAI                                   = 0xb
	CLOCK_THREAD_CPUTIME_ID                     = 0x3
	CLOCK_TXFROMRX                              = 0x4
	CLOCK_TXINT                                 = 0x3
	CLONE_ARGS_SIZE_VER0                        = 0x40
	CLONE_ARGS_SIZE_VER1                        = 0x50
	CLONE_ARGS_SIZE_VER2                        = 0x58
	CLONE_CHILD_CLEARTID                        = 0x200000
	CLONE_CHILD_SETTID                          = 0x1000000
	CLONE_CLEAR_SIGHAND                         = 0x100000000
	CLONE_DETACHED                              = 0x400000
	CLONE_FILES                                 = 0x400
	CLONE_FS                                    = 0x200
	CLONE_INTO_CGROUP                           = 0x200000000
	CLONE_IO                                    = 0x80000000
	CLONE_NEWCGROUP                             = 0x2000000
	CLONE_NEWIPC                                = 0x8000000
	CLONE_NEWNET                                = 0x40000000
	CLONE_NEWNS                                 = 0x20000
	CLONE_NEWPID                                = 0x20000000
	CLONE_NEWTIME                               = 0x80
	CLONE_NEWUSER                               = 0x10000000
	CLONE_NEWUTS                                = 0x4000000
	CLONE_PARENT                                = 0x8000
	CLONE_PARENT_SETTID                         = 0x100000
	CLONE_PIDFD                                 = 0x1000
	CLONE_PTRACE                                = 0x2000
	CLONE_SETTLS                                = 0x80000
	CLONE_SIGHAND                               = 0x800
	CLONE_SYSVSEM                               = 0x40000
	CLONE_THREAD                                = 0x10000
	CLONE_UNTRACED                              = 0x800000
	CLONE_VFORK                                 = 0x4000
	CLONE_VM                                    = 0x100
	CMSPAR                                      = 0x40000000
	CODA_SUPER_MAGIC                            = 0x73757245
	CR0                                         = 0x0
	CRAMFS_MAGIC                                = 0x28cd3d45
	CRTSCTS                                     = 0x80000000
	CRYPTO_MAX_NAME                             = 0x40
	CRYPTO_MSG_MAX                              = 0x15
	CRYPTO_NR_MSGTYPES                          = 0x6
	CRYPTO_REPORT_MAXSIZE                       = 0x160
	CS5                                         = 0x0
	CSIGNAL                                     = 0xff
	CSTART                                      = 0x11
	CSTATUS                                     = 0x0
	CSTOP                                       = 0x13
	CSUSP                                       = 0x1a
	DAXFS_MAGIC                                 = 0x64646178
	DEBUGFS_MAGIC                               = 0x64626720
	DEVLINK_CMD_ESWITCH_MODE_GET                = 0x1d
	DEVLINK_CMD_ESWITCH_MODE_SET                = 0x1e
	DEVLINK_FLASH_OVERWRITE_IDENTIFIERS         = 0x2
	DEVLINK_FLASH_OVERWRITE_SETTINGS            = 0x1
	DEVLINK_GENL_MCGRP_CONFIG_NAME              = "config"
	DEVLINK_GENL_NAME                           = "devlink"
	DEVLINK_GENL_VERSION                        = 0x1
	DEVLINK_PORT_FN_CAP_IPSEC_CRYPTO            = 0x4
	DEVLINK_PORT_FN_CAP_IPSEC_PACKET            = 0x8
	DEVLINK_PORT_FN_CAP_MIGRATABLE              = 0x2
	DEVLINK_PORT_FN_CAP_ROCE                    = 0x1
	DEVLINK_SB_THRESHOLD_TO_ALPHA_MAX           = 0x14
	DEVLINK_SUPPORTED_FLASH_OVERWRITE_SECTIONS  = 0x3
	DEVMEM_MAGIC                                = 0x454d444d
	DEVPTS_SUPER_MAGIC                          = 0x1cd1
	DMA_BUF_MAGIC                               = 0x444d4142
	DM_ACTIVE_PRESENT_FLAG                      = 0x20
	DM_BUFFER_FULL_FLAG                         = 0x100
	DM_CONTROL_NODE                             = "control"
	DM_DATA_OUT_FLAG                            = 0x10000
	DM_DEFERRED_REMOVE                          = 0x20000
	DM_DEV_ARM_POLL                             = 0xc138fd10
	DM_DEV_CREATE                               = 0xc138fd03
	DM_DEV_REMOVE                               = 0xc138fd04
	DM_DEV_RENAME                               = 0xc138fd05
	DM_DEV_SET_GEOMETRY                         = 0xc138fd0f
	DM_DEV_STATUS                               = 0xc138fd07
	DM_DEV_SUSPEND                              = 0xc138fd06
	DM_DEV_WAIT                                 = 0xc138fd08
	DM_DIR                                      = "mapper"
	DM_GET_TARGET_VERSION                       = 0xc138fd11
	DM_IMA_MEASUREMENT_FLAG                     = 0x80000
	DM_INACTIVE_PRESENT_FLAG                    = 0x40
	DM_INTERNAL_SUSPEND_FLAG                    = 0x40000
	DM_IOCTL                                    = 0xfd
	DM_LIST_DEVICES                             = 0xc138fd02
	DM_LIST_VERSIONS                            = 0xc138fd0d
	DM_MAX_TYPE_NAME                            = 0x10
	DM_NAME_LEN                                 = 0x80
	DM_NAME_LIST_FLAG_DOESNT_HAVE_UUID          = 0x2
	DM_NAME_LIST_FLAG_HAS_UUID                  = 0x1
	DM_NOFLUSH_FLAG                             = 0x800
	DM_PERSISTENT_DEV_FLAG                      = 0x8
	DM_QUERY_INACTIVE_TABLE_FLAG                = 0x1000
	DM_READONLY_FLAG                            = 0x1
	DM_REMOVE_ALL                               = 0xc138fd01
	DM_SECURE_DATA_FLAG                         = 0x8000
	DM_SKIP_BDGET_FLAG                          = 0x200
	DM_SKIP_LOCKFS_FLAG                         = 0x400
	DM_STATUS_TABLE_FLAG                        = 0x10
	DM_SUSPEND_FLAG                             = 0x2
	DM_TABLE_CLEAR                              = 0xc138fd0a
	DM_TABLE_DEPS                               = 0xc138fd0b
	DM_TABLE_LOAD                               = 0xc138fd09
	DM_TABLE_STATUS                             = 0xc138fd0c
	DM_TARGET_MSG                               = 0xc138fd0e
	DM_UEVENT_GENERATED_FLAG                    = 0x2000
	DM_UUID_FLAG                                = 0x4000
	DM_UUID_LEN                                 = 0x81
	DM_VERSION                                  = 0xc138fd00
	DM_VERSION_EXTRA                            = "-ioctl (2025-04-28)"
	DM_VERSION_MAJOR                            = 0x4
	DM_VERSION_MINOR                            = 0x32
	DM_VERSION_PATCHLEVEL                       = 0x0
	DT_BLK                                      = 0x6
	DT_CHR                                      = 0x2
	DT_DIR                                      = 0x4
	DT_FIFO                                     = 0x1
	DT_LNK                                      = 0xa
	DT_REG                                      = 0x8
	DT_SOCK                                     = 0xc
	DT_UNKNOWN                                  = 0x0
	DT_WHT                                      = 0xe
	ECHO                                        = 0x8
	ECRYPTFS_SUPER_MAGIC                        = 0xf15f
	EFD_SEMAPHORE                               = 0x1
	EFIVARFS_MAGIC                              = 0xde5e81e4
	EFS_SUPER_MAGIC                             = 0x414a53
	EM_386                                      = 0x3
	EM_486                                      = 0x6
	EM_68K                                      = 0x4
	EM_860                                      = 0x7
	EM_88K                                      = 0x5
	EM_AARCH64                                  = 0xb7
	EM_ALPHA                                    = 0x9026
	EM_ALTERA_NIOS2                             = 0x71
	EM_ARCOMPACT                                = 0x5d
	EM_ARCV2                                    = 0xc3
	EM_ARM                                      = 0x28
	EM_BLACKFIN                                 = 0x6a
	EM_BPF                                      = 0xf7
	EM_CRIS                                     = 0x4c
	EM_CSKY                                     = 0xfc
	EM_CYGNUS_M32R                              = 0x9041
	EM_CYGNUS_MN10300                           = 0xbeef
	EM_FRV                                      = 0x5441
	EM_H8_300                                   = 0x2e
	EM_HEXAGON                                  = 0xa4
	EM_IA_64                                    = 0x32
	EM_LOONGARCH                                = 0x102
	EM_M32                                      = 0x1
	EM_M32R                                     = 0x58
	EM_MICROBLAZE                               = 0xbd
	EM_MIPS                                     = 0x8
	EM_MIPS_RS3_LE                              = 0xa
	EM_MIPS_RS4_BE                              = 0xa
	EM_MN10300                                  = 0x59
	EM_NDS32                                    = 0xa7
	EM_NONE                                     = 0x0
	EM_OPENRISC                                 = 0x5c
	EM_PARISC                                   = 0xf
	EM_PPC                                      = 0x14
	EM_PPC64                                    = 0x15
	EM_RISCV                                    = 0xf3
	EM_S390                                     = 0x16
	EM_S390_OLD                                 = 0xa390
	EM_SH                                       = 0x2a
	EM_SPARC                                    = 0x2
	EM_SPARC32PLUS                              = 0x12
	EM_SPARCV9                                  = 0x2b
	EM_SPU                                      = 0x17
	EM_TILEGX                                   = 0xbf
	EM_TILEPRO                                  = 0xbc
	EM_TI_C6000                                 = 0x8c
	EM_UNICORE                                  = 0x6e
	EM_X86_64                                   = 0x3e
	EM_XTENSA                                   = 0x5e
	ENCODING_DEFAULT                            = 0x0
	ENCODING_FM_MARK                            = 0x3
	ENCODING_FM_SPACE                           = 0x4
	ENCODING_MANCHESTER                         = 0x5
	ENCODING_NRZ                                = 0x1
	ENCODING_NRZI                               = 0x2
	EPOLLERR                                    = 0x8
	EPOLLET                                     = 0x80000000
	EPOLLEXCLUSIVE                              = 0x10000000
	EPOLLHUP                                    = 0x10
	EPOLLIN                                     = 0x1
	EPOLLMSG                                    = 0x400
	EPOLLONESHOT                                = 0x40000000
	EPOLLOUT                                    = 0x4
	EPOLLPRI                                    = 0x2
	EPOLLRDBAND                                 = 0x80
	EPOLLRDHUP                                  = 0x2000
	EPOLLRDNORM                                 = 0x40
	EPOLLWAKEUP                                 = 0x20000000
	EPOLLWRBAND                                 = 0x200
	EPOLLWRNORM                                 = 0x100
	EPOLL_CTL_ADD                               = 0x1
	EPOLL_CTL_DEL                               = 0x2
	EPOLL_CTL_MOD                               = 0x3
	EPOLL_IOC_TYPE                              = 0x8a
	EROFS_SUPER_MAGIC_V1                        = 0xe0f5e1e2
	ETHTOOL_BUSINFO_LEN                         = 0x20
	ETHTOOL_EROMVERS_LEN                        = 0x20
	ETHTOOL_FAMILY_NAME                         = "ethtool"
	ETHTOOL_FAMILY_VERSION                      = 0x1
	ETHTOOL_FEC_AUTO                            = 0x2
	ETHTOOL_FEC_BASER                           = 0x10
	ETHTOOL_FEC_LLRS                            = 0x20
	ETHTOOL_FEC_NONE                            = 0x1
	ETHTOOL_FEC_OFF                             = 0x4
	ETHTOOL_FEC_RS                              = 0x8
	ETHTOOL_FLAG_ALL                            = 0x7
	ETHTOOL_FLASHDEV                            = 0x33
	ETHTOOL_FLASH_MAX_FILENAME                  = 0x80
	ETHTOOL_FWVERS_LEN                          = 0x20
	ETHTOOL_F_COMPAT                            = 0x4
	ETHTOOL_F_UNSUPPORTED                       = 0x1
	ETHTOOL_F_WISH                              = 0x2
	ETHTOOL_GCHANNELS                           = 0x3c
	ETHTOOL_GCOALESCE                           = 0xe
	ETHTOOL_GDRVINFO                            = 0x3
	ETHTOOL_GEEE                                = 0x44
	ETHTOOL_GEEPROM                             = 0xb
	ETHTOOL_GENL_NAME                           = "ethtool"
	ETHTOOL_GENL_VERSION                        = 0x1
	ETHTOOL_GET_DUMP_DATA                       = 0x40
	ETHTOOL_GET_DUMP_FLAG                       = 0x3f
	ETHTOOL_GET_TS_INFO                         = 0x41
	ETHTOOL_GFEATURES                           = 0x3a
	ETHTOOL_GFECPARAM                           = 0x50
	ETHTOOL_GFLAGS                              = 0x25
	ETHTOOL_GGRO                                = 0x2b
	ETHTOOL_GGSO                                = 0x23
	ETHTOOL_GLINK                               = 0xa
	ETHTOOL_GLINKSETTINGS                       = 0x4c
	ETHTOOL_GMODULEEEPROM                       = 0x43
	ETHTOOL_GMODULEINFO                         = 0x42
	ETHTOOL_GMSGLVL                             = 0x7
	ETHTOOL_GPAUSEPARAM                         = 0x12
	ETHTOOL_GPERMADDR                           = 0x20
	ETHTOOL_GPFLAGS                             = 0x27
	ETHTOOL_GPHYSTATS                           = 0x4a
	ETHTOOL_GREGS                               = 0x4
	ETHTOOL_GRINGPARAM                          = 0x10
	ETHTOOL_GRSSH                               = 0x46
	ETHTOOL_GRXCLSRLALL                         = 0x30
	ETHTOOL_GRXCLSRLCNT                         = 0x2e
	ETHTOOL_GRXCLSRULE                          = 0x2f
	ETHTOOL_GRXCSUM                             = 0x14
	ETHTOOL_GRXFH                               = 0x29
	ETHTOOL_GRXFHINDIR                          = 0x38
	ETHTOOL_GRXNTUPLE                           = 0x36
	ETHTOOL_GRXRINGS                            = 0x2d
	ETHTOOL_GSET                                = 0x1
	ETHTOOL_GSG                                 = 0x18
	ETHTOOL_GSSET_INFO                          = 0x37
	ETHTOOL_GSTATS                              = 0x1d
	ETHTOOL_GSTRINGS                            = 0x1b
	ETHTOOL_GTSO                                = 0x1e
	ETHTOOL_GTUNABLE                            = 0x48
	ETHTOOL_GTXCSUM                             = 0x16
	ETHTOOL_GUFO                                = 0x21
	ETHTOOL_GWOL                                = 0x5
	ETHTOOL_MCGRP_MONITOR_NAME                  = "monitor"
	ETHTOOL_NWAY_RST                            = 0x9
	ETHTOOL_PERQUEUE                            = 0x4b
	ETHTOOL_PHYS_ID                             = 0x1c
	ETHTOOL_PHY_EDPD_DFLT_TX_MSECS              = 0xffff
	ETHTOOL_PHY_EDPD_DISABLE                    = 0x0
	ETHTOOL_PHY_EDPD_NO_TX                      = 0xfffe
	ETHTOOL_PHY_FAST_LINK_DOWN_OFF              = 0xff
	ETHTOOL_PHY_FAST_LINK_DOWN_ON               = 0x0
	ETHTOOL_PHY_GTUNABLE                        = 0x4e
	ETHTOOL_PHY_STUNABLE                        = 0x4f
	ETHTOOL_RESET                               = 0x34
	ETHTOOL_RXNTUPLE_ACTION_CLEAR               = -0x2
	ETHTOOL_RXNTUPLE_ACTION_DROP                = -0x1
	ETHTOOL_RX_FLOW_SPEC_RING                   = 0xffffffff
	ETHTOOL_RX_FLOW_SPEC_RING_VF                = 0xff00000000
	ETHTOOL_RX_FLOW_SPEC_RING_VF_OFF            = 0x20
	ETHTOOL_SCHANNELS                           = 0x3d
	ETHTOOL_SCOALESCE                           = 0xf
	ETHTOOL_SEEE                                = 0x45
	ETHTOOL_SEEPROM                             = 0xc
	ETHTOOL_SET_DUMP                            = 0x3e
	ETHTOOL_SFEATURES                           = 0x3b
	ETHTOOL_SFECPARAM                           = 0x51
	ETHTOOL_SFLAGS                              = 0x26
	ETHTOOL_SGRO                                = 0x2c
	ETHTOOL_SGSO                                = 0x24
	ETHTOOL_SLINKSETTINGS                       = 0x4d
	ETHTOOL_SMSGLVL                             = 0x8
	ETHTOOL_SPAUSEPARAM                         = 0x13
	ETHTOOL_SPFLAGS                             = 0x28
	ETHTOOL_SRINGPARAM                          = 0x11
	ETHTOOL_SRSSH                               = 0x47
	ETHTOOL_SRXCLSRLDEL                         = 0x31
	ETHTOOL_SRXCLSRLINS                         = 0x32
	ETHTOOL_SRXCSUM                             = 0x15
	ETHTOOL_SRXFH                               = 0x2a
	ETHTOOL_SRXFHINDIR                          = 0x39
	ETHTOOL_SRXNTUPLE                           = 0x35
	ETHTOOL_SSET                                = 0x2
	ETHTOOL_SSG                                 = 0x19
	ETHTOOL_STSO                                = 0x1f
	ETHTOOL_STUNABLE                            = 0x49
	ETHTOOL_STXCSUM                             = 0x17
	ETHTOOL_SUFO                                = 0x22
	ETHTOOL_SWOL                                = 0x6
	ETHTOOL_TEST                                = 0x1a
	ETH_P_1588                                  = 0x88f7
	ETH_P_8021AD                                = 0x88a8
	ETH_P_8021AH                                = 0x88e7
	ETH_P_8021Q                                 = 0x8100
	ETH_P_80221                                 = 0x8917
	ETH_P_802_2                                 = 0x4
	ETH_P_802_3                                 = 0x1
	ETH_P_802_3_MIN                             = 0x600
	ETH_P_802_EX1                               = 0x88b5
	ETH_P_AARP                                  = 0x80f3
	ETH_P_AF_IUCV                               = 0xfbfb
	ETH_P_ALL                                   = 0x3
	ETH_P_AOE                                   = 0x88a2
	ETH_P_ARCNET                                = 0x1a
	ETH_P_ARP                                   = 0x806
	ETH_P_ATALK                                 = 0x809b
	ETH_P_ATMFATE                               = 0x8884
	ETH_P_ATMMPOA                               = 0x884c
	ETH_P_AX25                                  = 0x2
	ETH_P_BATMAN                                = 0x4305
	ETH_P_BPQ                                   = 0x8ff
	ETH_P_CAIF                                  = 0xf7
	ETH_P_CAN                                   = 0xc
	ETH_P_CANFD                                 = 0xd
	ETH_P_CANXL                                 = 0xe
	ETH_P_CFM                                   = 0x8902
	ETH_P_CONTROL                               = 0x16
	ETH_P_CUST                                  = 0x6006
	ETH_P_DDCMP                                 = 0x6
	ETH_P_DEC                                   = 0x6000
	ETH_P_DIAG                                  = 0x6005
	ETH_P_DNA_DL                                = 0x6001
	ETH_P_DNA_RC                                = 0x6002
	ETH_P_DNA_RT                                = 0x6003
	ETH_P_DSA                                   = 0x1b
	ETH_P_DSA_8021Q                             = 0xdadb
	ETH_P_DSA_A5PSW                             = 0xe001
	ETH_P_ECONET                                = 0x18
	ETH_P_EDSA                                  = 0xdada
	ETH_P_ERSPAN                                = 0x88be
	ETH_P_ERSPAN2                               = 0x22eb
	ETH_P_ETHERCAT                              = 0x88a4
	ETH_P_FCOE                                  = 0x8906
	ETH_P_FIP                                   = 0x8914
	ETH_P_HDLC                                  = 0x19
	ETH_P_HSR                                   = 0x892f
	ETH_P_IBOE                                  = 0x8915
	ETH_P_IEEE802154                            = 0xf6
	ETH_P_IEEEPUP                               = 0xa00
	ETH_P_IEEEPUPAT                             = 0xa01
	ETH_P_IFE                                   = 0xed3e
	ETH_P_IP                                    = 0x800
	ETH_P_IPV6                                  = 0x86dd
	ETH_P_IPX                                   = 0x8137
	ETH_P_IRDA                                  = 0x17
	ETH_P_LAT                                   = 0x6004
	ETH_P_LINK_CTL                              = 0x886c
	ETH_P_LLDP                                  = 0x88cc
	ETH_P_LOCALTALK                             = 0x9
	ETH_P_LOOP                                  = 0x60
	ETH_P_LOOPBACK                              = 0x9000
	ETH_P_MACSEC                                = 0x88e5
	ETH_P_MAP                                   = 0xf9
	ETH_P_MCTP                                  = 0xfa
	ETH_P_MOBITEX                               = 0x15
	ETH_P_MPLS_MC                               = 0x8848
	ETH_P_MPLS_UC                               = 0x8847
	ETH_P_MRP                                   = 0x88e3
	ETH_P_MVRP                                  = 0x88f5
	ETH_P_NCSI                                  = 0x88f8
	ETH_P_NSH                                   = 0x894f
	ETH_P_PAE                                   = 0x888e
	ETH_P_PAUSE                                 = 0x8808
	ETH_P_PHONET                                = 0xf5
	ETH_P_PPPTALK                               = 0x10
	ETH_P_PPP_DISC                              = 0x8863
	ETH_P_PPP_MP                                = 0x8
	ETH_P_PPP_SES                               = 0x8864
	ETH_P_PREAUTH                               = 0x88c7
	ETH_P_PROFINET                              = 0x8892
	ETH_P_PRP                                   = 0x88fb
	ETH_P_PUP                                   = 0x200
	ETH_P_PUPAT                                 = 0x201
	ETH_P_QINQ1                                 = 0x9100
	ETH_P_QINQ2                                 = 0x9200
	ETH_P_QINQ3                                 = 0x9300
	ETH_P_RARP                                  = 0x8035
	ETH_P_REALTEK                               = 0x8899
	ETH_P_SCA                                   = 0x6007
	ETH_P_SLOW                                  = 0x8809
	ETH_P_SNAP                                  = 0x5
	ETH_P_TDLS                                  = 0x890d
	ETH_P_TEB                                   = 0x6558
	ETH_P_TIPC                                  = 0x88ca
	ETH_P_TRAILER                               = 0x1c
	ETH_P_TR_802_2                              = 0x11
	ETH_P_TSN                                   = 0x22f0
	ETH_P_WAN_PPP                               = 0x7
	ETH_P_WCCP                                  = 0x883e
	ETH_P_X25                                   = 0x805
	ETH_P_XDSA                                  = 0xf8
	EV_ABS                                      = 0x3
	EV_CNT                                      = 0x20
	EV_FF                                       = 0x15
	EV_FF_STATUS                                = 0x17
	EV_KEY                                      = 0x1
	EV_LED                                      = 0x11
	EV_MAX                                      = 0x1f
	EV_MSC                                      = 0x4
	EV_PWR                                      = 0x16
	EV_REL                                      = 0x2
	EV_REP                                      = 0x14
	EV_SND                                      = 0x12
	EV_SW                                       = 0x5
	EV_SYN                                      = 0x0
	EV_VERSION                                  = 0x10001
	EXABYTE_ENABLE_NEST                         = 0xf0
	EXFAT_SUPER_MAGIC                           = 0x2011bab0
	EXT2_SUPER_MAGIC                            = 0xef53
	EXT3_SUPER_MAGIC                            = 0xef53
	EXT4_SUPER_MAGIC                            = 0xef53
	EXTA                                        = 0xe
	EXTB                                        = 0xf
	F2FS_SUPER_MAGIC                            = 0xf2f52010
	FALLOC_FL_ALLOCATE_RANGE                    = 0x0
	FALLOC_FL_COLLAPSE_RANGE                    = 0x8
	FALLOC_FL_INSERT_RANGE                      = 0x20
	FALLOC_FL_KEEP_SIZE                         = 0x1
	FALLOC_FL_NO_HIDE_STALE                     = 0x4
	FALLOC_FL_PUNCH_HOLE                        = 0x2
	FALLOC_FL_UNSHARE_RANGE                     = 0x40
	FALLOC_FL_ZERO_RANGE                        = 0x10
	FANOTIFY_METADATA_VERSION                   = 0x3
	FAN_ACCESS                                  = 0x1
	FAN_ACCESS_PERM                             = 0x20000
	FAN_ALLOW                                   = 0x1
	FAN_ALL_CLASS_BITS                          = 0xc
	FAN_ALL_EVENTS                              = 0x3b
	FAN_ALL_INIT_FLAGS                          = 0x3f
	FAN_ALL_MARK_FLAGS                          = 0xff
	FAN_ALL_OUTGOING_EVENTS                     = 0x3403b
	FAN_ALL_PERM_EVENTS                         = 0x30000
	FAN_ATTRIB                                  = 0x4
	FAN_AUDIT                                   = 0x10
	FAN_CLASS_CONTENT                           = 0x4
	FAN_CLASS_NOTIF                             = 0x0
	FAN_CLASS_PRE_CONTENT                       = 0x8
	FAN_CLOEXEC                                 = 0x1
	FAN_CLOSE                                   = 0x18
	FAN_CLOSE_NOWRITE                           = 0x10
	FAN_CLOSE_WRITE                             = 0x8
	FAN_CREATE                                  = 0x100
	FAN_DELETE                                  = 0x200
	FAN_DELETE_SELF                             = 0x400
	FAN_DENY                                    = 0x2
	FAN_ENABLE_AUDIT                            = 0x40
	FAN_EPIDFD                                  = -0x2
	FAN_ERRNO_BITS                              = 0x8
	FAN_ERRNO_MASK                              = 0xff
	FAN_ERRNO_SHIFT                             = 0x18
	FAN_EVENT_INFO_TYPE_DFID                    = 0x3
	FAN_EVENT_INFO_TYPE_DFID_NAME               = 0x2
	FAN_EVENT_INFO_TYPE_ERROR                   = 0x5
	FAN_EVENT_INFO_TYPE_FID                     = 0x1
	FAN_EVENT_INFO_TYPE_MNT                     = 0x7
	FAN_EVENT_INFO_TYPE_NEW_DFID_NAME           = 0xc
	FAN_EVENT_INFO_TYPE_OLD_DFID_NAME           = 0xa
	FAN_EVENT_INFO_TYPE_PIDFD                   = 0x4
	FAN_EVENT_INFO_TYPE_RANGE                   = 0x6
	FAN_EVENT_METADATA_LEN                      = 0x18
	FAN_EVENT_ON_CHILD                          = 0x8000000
	FAN_FS_ERROR                                = 0x8000
	FAN_INFO                                    = 0x20
	FAN_MARK_ADD                                = 0x1
	FAN_MARK_DONT_FOLLOW                        = 0x4
	FAN_MARK_EVICTABLE                          = 0x200
	FAN_MARK_FILESYSTEM                         = 0x100
	FAN_MARK_FLUSH                              = 0x80
	FAN_MARK_IGNORE                             = 0x400
	FAN_MARK_IGNORED_MASK                       = 0x20
	FAN_MARK_IGNORED_SURV_MODIFY                = 0x40
	FAN_MARK_IGNORE_SURV                        = 0x440
	FAN_MARK_INODE                              = 0x0
	FAN_MARK_MNTNS                              = 0x110
	FAN_MARK_MOUNT                              = 0x10
	FAN_MARK_ONLYDIR                            = 0x8
	FAN_MARK_REMOVE                             = 0x2
	FAN_MNT_ATTACH                              = 0x1000000
	FAN_MNT_DETACH                              = 0x2000000
	FAN_MODIFY                                  = 0x2
	FAN_MOVE                                    = 0xc0
	FAN_MOVED_FROM                              = 0x40
	FAN_MOVED_TO                                = 0x80
	FAN_MOVE_SELF                               = 0x800
	FAN_NOFD                                    = -0x1
	FAN_NONBLOCK                                = 0x2
	FAN_NOPIDFD                                 = -0x1
	FAN_ONDIR                                   = 0x40000000
	FAN_OPEN                                    = 0x20
	FAN_OPEN_EXEC                               = 0x1000
	FAN_OPEN_EXEC_PERM                          = 0x40000
	FAN_OPEN_PERM                               = 0x10000
	FAN_PRE_ACCESS                              = 0x100000
	FAN_Q_OVERFLOW                              = 0x4000
	FAN_RENAME                                  = 0x10000000
	FAN_REPORT_DFID_NAME                        = 0xc00
	FAN_REPORT_DFID_NAME_TARGET                 = 0x1e00
	FAN_REPORT_DIR_FID                          = 0x400
	FAN_REPORT_FD_ERROR                         = 0x2000
	FAN_REPORT_FID                              = 0x200
	FAN_REPORT_MNT                              = 0x4000
	FAN_REPORT_NAME                             = 0x800
	FAN_REPORT_PIDFD                            = 0x80
	FAN_REPORT_TARGET_FID                       = 0x1000
	FAN_REPORT_TID                              = 0x100
	FAN_RESPONSE_INFO_AUDIT_RULE                = 0x1
	FAN_RESPONSE_INFO_NONE                      = 0x0
	FAN_UNLIMITED_MARKS                         = 0x20
	FAN_UNLIMITED_QUEUE                         = 0x10
	FD_CLOEXEC                                  = 0x1
	FD_SETSIZE                                  = 0x400
	FF0                                         = 0x0
	FIB_RULE_DEV_DETACHED                       = 0x8
	FIB_RULE_FIND_SADDR                         = 0x10000
	FIB_RULE_IIF_DETACHED                       = 0x8
	FIB_RULE_INVERT                             = 0x2
	FIB_RULE_OIF_DETACHED                       = 0x10
	FIB_RULE_PERMANENT                          = 0x1
	FIB_RULE_UNRESOLVED                         = 0x4
	FIDEDUPERANGE                               = 0xc0189436
	FSCRYPT_ADD_KEY_FLAG_HW_WRAPPED             = 0x1
	FSCRYPT_KEY_DESCRIPTOR_SIZE                 = 0x8
	FSCRYPT_KEY_DESC_PREFIX                     = "fscrypt:"
	FSCRYPT_KEY_DESC_PREFIX_SIZE                = 0x8
	FSCRYPT_KEY_IDENTIFIER_SIZE                 = 0x10
	FSCRYPT_KEY_REMOVAL_STATUS_FLAG_FILES_BUSY  = 0x1
	FSCRYPT_KEY_REMOVAL_STATUS_FLAG_OTHER_USERS = 0x2
	FSCRYPT_KEY_SPEC_TYPE_DESCRIPTOR            = 0x1
	FSCRYPT_KEY_SPEC_TYPE_IDENTIFIER            = 0x2
	FSCRYPT_KEY_STATUS_ABSENT                   = 0x1
	FSCRYPT_KEY_STATUS_FLAG_ADDED_BY_SELF       = 0x1
	FSCRYPT_KEY_STATUS_INCOMPLETELY_REMOVED     = 0x3
	FSCRYPT_KEY_STATUS_PRESENT                  = 0x2
	FSCRYPT_MAX_KEY_SIZE                        = 0x40
	FSCRYPT_MODE_ADIANTUM                       = 0x9
	FSCRYPT_MODE_AES_128_CBC                    = 0x5
	FSCRYPT_MODE_AES_128_CTS                    = 0x6
	FSCRYPT_MODE_AES_256_CTS                    = 0x4
	FSCRYPT_MODE_AES_256_HCTR2                  = 0xa
	FSCRYPT_MODE_AES_256_XTS                    = 0x1
	FSCRYPT_MODE_SM4_CTS                        = 0x8
	FSCRYPT_MODE_SM4_XTS                        = 0x7
	FSCRYPT_POLICY_FLAGS_PAD_16                 = 0x2
	FSCRYPT_POLICY_FLAGS_PAD_32                 = 0x3
	FSCRYPT_POLICY_FLAGS_PAD_4                  = 0x0
	FSCRYPT_POLICY_FLAGS_PAD_8                  = 0x1
	FSCRYPT_POLICY_FLAGS_PAD_MASK               = 0x3
	FSCRYPT_POLICY_FLAG_DIRECT_KEY              = 0x4
	FSCRYPT_POLICY_FLAG_IV_INO_LBLK_32          = 0x10
	FSCRYPT_POLICY_FLAG_IV_INO_LBLK_64          = 0x8
	FSCRYPT_POLICY_V1                           = 0x0
	FSCRYPT_POLICY_V2                           = 0x2
	FS_ENCRYPTION_MODE_ADIANTUM                 = 0x9
	FS_ENCRYPTION_MODE_AES_128_CBC              = 0x5
	FS_ENCRYPTION_MODE_AES_128_CTS              = 0x6
	FS_ENCRYPTION_MODE_AES_256_CBC              = 0x3
	FS_ENCRYPTION_MODE_AES_256_CTS              = 0x4
	FS_ENCRYPTION_MODE_AES_256_GCM              = 0x2
	FS_ENCRYPTION_MODE_AES_256_XTS              = 0x1
	FS_ENCRYPTION_MODE_INVALID                  = 0x0
	FS_IOC_ADD_ENCRYPTION_KEY                   = 0xc0506617
	FS_IOC_GET_ENCRYPTION_KEY_STATUS            = 0xc080661a
	FS_IOC_GET_ENCRYPTION_POLICY_EX             = 0xc0096616
	FS_IOC_MEASURE_VERITY                       = 0xc0046686
	FS_IOC_READ_VERITY_METADATA                 = 0xc0286687
	FS_IOC_REMOVE_ENCRYPTION_KEY                = 0xc0406618
	FS_IOC_REMOVE_ENCRYPTION_KEY_ALL_USERS      = 0xc0406619
	FS_KEY_DESCRIPTOR_SIZE                      = 0x8
	FS_KEY_DESC_PREFIX                          = "fscrypt:"
	FS_KEY_DESC_PREFIX_SIZE                     = 0x8
	FS_MAX_KEY_SIZE                             = 0x40
	FS_POLICY_FLAGS_PAD_16                      = 0x2
	FS_POLICY_FLAGS_PAD_32                      = 0x3
	FS_POLICY_FLAGS_PAD_4                       = 0x0
	FS_POLICY_FLAGS_PAD_8                       = 0x1
	FS_POLICY_FLAGS_PAD_MASK                    = 0x3
	FS_POLICY_FLAGS_VALID                       = 0x7
	FS_VERITY_FL                                = 0x100000
	FS_VERITY_HASH_ALG_SHA256                   = 0x1
	FS_VERITY_HASH_ALG_SHA512                   = 0x2
	FS_VERITY_METADATA_TYPE_DESCRIPTOR          = 0x2
	FS_VERITY_METADATA_TYPE_MERKLE_TREE         = 0x1
	FS_VERITY_METADATA_TYPE_SIGNATURE           = 0x3
	FUSE_SUPER_MAGIC                            = 0x65735546
	FUTEXFS_SUPER_MAGIC                         = 0xbad1dea
	F_ADD_SEALS                                 = 0x409
	F_CREATED_QUERY                             = 0x404
	F_DUPFD                                     = 0x0
	F_DUPFD_CLOEXEC                             = 0x406
	F_DUPFD_QUERY                               = 0x403
	F_EXLCK                                     = 0x4
	F_GETFD                                     = 0x1
	F_GETFL                                     = 0x3
	F_GETLEASE                                  = 0x401
	F_GETOWN_EX                                 = 0x10
	F_GETPIPE_SZ                                = 0x408
	F_GETSIG                                    = 0xb
	F_GET_FILE_RW_HINT                          = 0x40d
	F_GET_RW_HINT                               = 0x40b
	F_GET_SEALS                                 = 0x40a
	F_LOCK                                      = 0x1
	F_NOTIFY                                    = 0x402
	F_OFD_GETLK                                 = 0x24
	F_OFD_SETLK                                 = 0x25
	F_OFD_SETLKW                                = 0x26
	F_OK                                        = 0x0
	F_SEAL_EXEC                                 = 0x20
	F_SEAL_FUTURE_WRITE                         = 0x10
	F_SEAL_GROW                                 = 0x4
	F_SEAL_SEAL                                 = 0x1
	F_SEAL_SHRINK                               = 0x2
	F_SEAL_WRITE                                = 0x8
	F_SETFD                                     = 0x2
	F_SETFL                                     = 0x4
	F_SETLEASE                                  = 0x400
	F_SETOWN_EX                                 = 0xf
	F_SETPIPE_SZ                                = 0x407
	F_SETSIG                                    = 0xa
	F_SET_FILE_RW_HINT                          = 0x40e
	F_SET_RW_HINT                               = 0x40c
	F_SHLCK                                     = 0x8
	F_TEST                                      = 0x3
	F_TLOCK                                     = 0x2
	F_ULOCK                                     = 0x0
	GENL_ADMIN_PERM                             = 0x1
	GENL_CMD_CAP_DO                             = 0x2
	GENL_CMD_CAP_DUMP                           = 0x4
	GENL_CMD_CAP_HASPOL                         = 0x8
	GENL_HDRLEN                                 = 0x4
	GENL_ID_CTRL                                = 0x10
	GENL_ID_PMCRAID                             = 0x12
	GENL_ID_VFS_DQUOT                           = 0x11
	GENL_MAX_ID                                 = 0x3ff
	GENL_MIN_ID                                 = 0x10
	GENL_NAMSIZ                                 = 0x10
	GENL_START_ALLOC                            = 0x13
	GENL_UNS_ADMIN_PERM                         = 0x10
	GRND_INSECURE                               = 0x4
	GRND_NONBLOCK                               = 0x1
	GRND_RANDOM                                 = 0x2
	HDIO_DRIVE_CMD                              = 0x31f
	HDIO_DRIVE_CMD_AEB                          = 0x31e
	HDIO_DRIVE_CMD_HDR_SIZE                     = 0x4
	HDIO_DRIVE_HOB_HDR_SIZE                     = 0x8
	HDIO_DRIVE_RESET                            = 0x31c
	HDIO_DRIVE_TASK                             = 0x31e
	HDIO_DRIVE_TASKFILE                         = 0x31d
	HDIO_DRIVE_TASK_HDR_SIZE                    = 0x8
	HDIO_GETGEO                                 = 0x301
	HDIO_GET_32BIT                              = 0x309
	HDIO_GET_ACOUSTIC                           = 0x30f
	HDIO_GET_ADDRESS                            = 0x310
	HDIO_GET_BUSSTATE                           = 0x31a
	HDIO_GET_DMA                                = 0x30b
	HDIO_GET_IDENTITY                           = 0x30d
	HDIO_GET_KEEPSETTINGS                       = 0x308
	HDIO_GET_MULTCOUNT                          = 0x304
	HDIO_GET_NICE                               = 0x30c
	HDIO_GET_NOWERR                             = 0x30a
	HDIO_GET_QDMA                               = 0x305
	HDIO_GET_UNMASKINTR                         = 0x302
	HDIO_GET_WCACHE                             = 0x30e
	HDIO_OBSOLETE_IDENTITY                      = 0x307
	HDIO_SCAN_HWIF                              = 0x328
	HDIO_SET_32BIT                              = 0x324
	HDIO_SET_ACOUSTIC                           = 0x32c
	HDIO_SET_ADDRESS                            = 0x32f
	HDIO_SET_BUSSTATE                           = 0x32d
	HDIO_SET_DMA                                = 0x326
	HDIO_SET_KEEPSETTINGS                       = 0x323
	HDIO_SET_MULTCOUNT                          = 0x321
	HDIO_SET_NICE                               = 0x329
	HDIO_SET_NOWERR                             = 0x325
	HDIO_SET_PIO_MODE                           = 0x327
	HDIO_SET_QDMA                               = 0x32e
	HDIO_SET_UNMASKINTR                         = 0x322
	HDIO_SET_WCACHE                             = 0x32b
	HDIO_SET_XFER                               = 0x306
	HDIO_TRISTATE_HWIF                          = 0x31b
	HDIO_UNREGISTER_HWIF                        = 0x32a
	HID_MAX_DESCRIPTOR_SIZE                     = 0x1000
	HOSTFS_SUPER_MAGIC                          = 0xc0ffee
	HPFS_SUPER_MAGIC                            = 0xf995e849
	HUGETLBFS_MAGIC                             = 0x958458f6
	IBSHIFT                                     = 0x10
	ICRNL                                       = 0x100
	IFA_F_DADFAILED                             = 0x8
	IFA_F_DEPRECATED                            = 0x20
	IFA_F_HOMEADDRESS                           = 0x10
	IFA_F_MANAGETEMPADDR                        = 0x100
	IFA_F_MCAUTOJOIN                            = 0x400
	IFA_F_NODAD                                 = 0x2
	IFA_F_NOPREFIXROUTE                         = 0x200
	IFA_F_OPTIMISTIC                            = 0x4
	IFA_F_PERMANENT                             = 0x80
	IFA_F_SECONDARY                             = 0x1
	IFA_F_STABLE_PRIVACY                        = 0x800
	IFA_F_TEMPORARY                             = 0x1
	IFA_F_TENTATIVE                             = 0x40
	IFA_MAX                                     = 0xb
	IFF_ALLMULTI                                = 0x200
	IFF_ATTACH_QUEUE                            = 0x200
	IFF_AUTOMEDIA                               = 0x4000
	IFF_BROADCAST                               = 0x2
	IFF_DEBUG                                   = 0x4
	IFF_DETACH_QUEUE                            = 0x400
	IFF_DORMANT                                 = 0x20000
	IFF_DYNAMIC                                 = 0x8000
	IFF_ECHO                                    = 0x40000
	IFF_LOOPBACK                                = 0x8
	IFF_LOWER_UP                                = 0x10000
	IFF_MASTER                                  = 0x400
	IFF_MULTICAST                               = 0x1000
	IFF_MULTI_QUEUE                             = 0x100
	IFF_NAPI                                    = 0x10
	IFF_NAPI_FRAGS                              = 0x20
	IFF_NOARP                                   = 0x80
	IFF_NOFILTER                                = 0x1000
	IFF_NOTRAILERS                              = 0x20
	IFF_NO_CARRIER                              = 0x40
	IFF_NO_PI                                   = 0x1000
	IFF_ONE_QUEUE                               = 0x2000
	IFF_PERSIST                                 = 0x800
	IFF_POINTOPOINT                             = 0x10
	IFF_PORTSEL                                 = 0x2000
	IFF_PROMISC                                 = 0x100
	IFF_RUNNING                                 = 0x40
	IFF_SLAVE                                   = 0x800
	IFF_TAP                                     = 0x2
	IFF_TUN                                     = 0x1
	IFF_TUN_EXCL                                = 0x8000
	IFF_UP                                      = 0x1
	IFF_VNET_HDR                                = 0x4000
	IFF_VOLATILE                                = 0x70c5a
	IFNAMSIZ                                    = 0x10
	IGNBRK                                      = 0x1
	IGNCR                                       = 0x80
	IGNPAR                                      = 0x4
	IMAXBEL                                     = 0x2000
	INLCR                                       = 0x40
	INPCK                                       = 0x10
	IN_ACCESS                                   = 0x1
	IN_ALL_EVENTS                               = 0xfff
	IN_ATTRIB                                   = 0x4
	IN_CLASSA_HOST                              = 0xffffff
	IN_CLASSA_MAX                               = 0x80
	IN_CLASSA_NET                               = 0xff000000
	IN_CLASSA_NSHIFT                            = 0x18
	IN_CLASSB_HOST                              = 0xffff
	IN_CLASSB_MAX                               = 0x10000
	IN_CLASSB_NET                               = 0xffff0000
	IN_CLASSB_NSHIFT                            = 0x10
	IN_CLASSC_HOST                              = 0xff
	IN_CLASSC_NET                               = 0xffffff00
	IN_CLASSC_NSHIFT                            = 0x8
	IN_CLOSE                                    = 0x18
	IN_CLOSE_NOWRITE                            = 0x10
	IN_CLOSE_WRITE                              = 0x8
	IN_CREATE                                   = 0x100
	IN_DELETE                                   = 0x200
	IN_DELETE_SELF                              = 0x400
	IN_DONT_FOLLOW                              = 0x2000000
	IN_EXCL_UNLINK                              = 0x4000000
	IN_IGNORED                                  = 0x8000
	IN_ISDIR                                    = 0x40000000
	IN_LOOPBACKNET                              = 0x7f
	IN_MASK_ADD                                 = 0x20000000
	IN_MASK_CREATE                              = 0x10000000
	IN_MODIFY                                   = 0x2
	IN_MOVE                                     = 0xc0
	IN_MOVED_FROM                               = 0x40
	IN_MOVED_TO                                 = 0x80
	IN_MOVE_SELF                                = 0x800
	IN_ONESHOT                                  = 0x80000000
	IN_ONLYDIR                                  = 0x1000000
	IN_OPEN                                     = 0x20
	IN_Q_OVERFLOW                               = 0x4000
	IN_UNMOUNT                                  = 0x2000
	IPPROTO_AH                                  = 0x33
	IPPROTO_BEETPH                              = 0x5e
	IPPROTO_COMP                                = 0x6c
	IPPROTO_DCCP                                = 0x21
	IPPROTO_DSTOPTS                             = 0x3c
	IPPROTO_EGP                                 = 0x8
	IPPROTO_ENCAP                               = 0x62
	IPPROTO_ESP                                 = 0x32
	IPPROTO_ETHERNET                            = 0x8f
	IPPROTO_FRAGMENT                            = 0x2c
	IPPROTO_GRE                                 = 0x2f
	IPPROTO_HOPOPTS                             = 0x0
	IPPROTO_ICMP                                = 0x1
	IPPROTO_ICMPV6                              = 0x3a
	IPPROTO_IDP                                 = 0x16
	IPPROTO_IGMP                                = 0x2
	IPPROTO_IP                                  = 0x0
	IPPROTO_IPIP                                = 0x4
	IPPROTO_IPV6                                = 0x29
	IPPROTO_L2TP                                = 0x73
	IPPROTO_MH                                  = 0x87
	IPPROTO_MPLS                                = 0x89
	IPPROTO_MPTCP                               = 0x106
	IPPROTO_MTP                                 = 0x5c
	IPPROTO_NONE                                = 0x3b
	IPPROTO_PIM                                 = 0x67
	IPPROTO_PUP                                 = 0xc
	IPPROTO_RAW                                 = 0xff
	IPPROTO_ROUTING                             = 0x2b
	IPPROTO_RSVP                                = 0x2e
	IPPROTO_SCTP                                = 0x84
	IPPROTO_SMC                                 = 0x100
	IPPROTO_TCP                                 = 0x6
	IPPROTO_TP                                  = 0x1d
	IPPROTO_UDP                                 = 0x11
	IPPROTO_UDPLITE                             = 0x88
	IPV6_2292DSTOPTS                            = 0x4
	IPV6_2292HOPLIMIT                           = 0x8
	IPV6_2292HOPOPTS                            = 0x3
	IPV6_2292PKTINFO                            = 0x2
	IPV6_2292PKTOPTIONS                         = 0x6
	IPV6_2292RTHDR                              = 0x5
	IPV6_ADDRFORM                               = 0x1
	IPV6_ADDR_PREFERENCES                       = 0x48
	IPV6_ADD_MEMBERSHIP                         = 0x14
	IPV6_AUTHHDR                                = 0xa
	IPV6_AUTOFLOWLABEL                          = 0x46
	IPV6_CHECKSUM                               = 0x7
	IPV6_DONTFRAG                               = 0x3e
	IPV6_DROP_MEMBERSHIP                        = 0x15
	IPV6_DSTOPTS                                = 0x3b
	IPV6_FREEBIND                               = 0x4e
	IPV6_HDRINCL                                = 0x24
	IPV6_HOPLIMIT                               = 0x34
	IPV6_HOPOPTS                                = 0x36
	IPV6_IPSEC_POLICY                           = 0x22
	IPV6_JOIN_ANYCAST                           = 0x1b
	IPV6_JOIN_GROUP                             = 0x14
	IPV6_LEAVE_ANYCAST                          = 0x1c
	IPV6_LEAVE_GROUP                            = 0x15
	IPV6_MINHOPCOUNT                            = 0x49
	IPV6_MTU                                    = 0x18
	IPV6_MTU_DISCOVER                           = 0x17
	IPV6_MULTICAST_ALL                          = 0x1d
	IPV6_MULTICAST_HOPS                         = 0x12
	IPV6_MULTICAST_IF                           = 0x11
	IPV6_MULTICAST_LOOP                         = 0x13
	IPV6_NEXTHOP                                = 0x9
	IPV6_ORIGDSTADDR                            = 0x4a
	IPV6_PATHMTU                                = 0x3d
	IPV6_PKTINFO                                = 0x32
	IPV6_PMTUDISC_DO                            = 0x2
	IPV6_PMTUDISC_DONT                          = 0x0
	IPV6_PMTUDISC_INTERFACE                     = 0x4
	IPV6_PMTUDISC_OMIT                          = 0x5
	IPV6_PMTUDISC_PROBE                         = 0x3
	IPV6_PMTUDISC_WANT                          = 0x1
	IPV6_RECVDSTOPTS                            = 0x3a
	IPV6_RECVERR                                = 0x19
	IPV6_RECVERR_RFC4884                        = 0x1f
	IPV6_RECVFRAGSIZE                           = 0x4d
	IPV6_RECVHOPLIMIT                           = 0x33
	IPV6_RECVHOPOPTS                            = 0x35
	IPV6_RECVORIGDSTADDR                        = 0x4a
	IPV6_RECVPATHMTU                            = 0x3c
	IPV6_RECVPKTINFO                            = 0x31
	IPV6_RECVRTHDR                              = 0x38
	IPV6_RECVTCLASS                             = 0x42
	IPV6_ROUTER_ALERT                           = 0x16
	IPV6_ROUTER_ALERT_ISOLATE                   = 0x1e
	IPV6_RTHDR                                  = 0x39
	IPV6_RTHDRDSTOPTS                           = 0x37
	IPV6_RTHDR_LOOSE                            = 0x0
	IPV6_RTHDR_STRICT                           = 0x1
	IPV6_RTHDR_TYPE_0                           = 0x0
	IPV6_RXDSTOPTS                              = 0x3b
	IPV6_RXHOPOPTS                              = 0x36
	IPV6_TCLASS                                 = 0x43
	IPV6_TRANSPARENT                            = 0x4b
	IPV6_UNICAST_HOPS                           = 0x10
	IPV6_UNICAST_IF                             = 0x4c
	IPV6_V6ONLY                                 = 0x1a
	IPV6_VERSION                                = 0x60
	IPV6_VERSION_MASK                           = 0xf0
	IPV6_XFRM_POLICY                            = 0x23
	IP_ADD_MEMBERSHIP                           = 0x23
	IP_ADD_SOURCE_MEMBERSHIP                    = 0x27
	IP_BIND_ADDRESS_NO_PORT                     = 0x18
	IP_BLOCK_SOURCE                             = 0x26
	IP_CHECKSUM                                 = 0x17
	IP_DEFAULT_MULTICAST_LOOP                   = 0x1
	IP_DEFAULT_MULTICAST_TTL                    = 0x1
	IP_DF                                       = 0x4000
	IP_DROP_MEMBERSHIP                          = 0x24
	IP_DROP_SOURCE_MEMBERSHIP                   = 0x28
	IP_FREEBIND                                 = 0xf
	IP_HDRINCL                                  = 0x3
	IP_IPSEC_POLICY                             = 0x10
	IP_LOCAL_PORT_RANGE                         = 0x33
	IP_MAXPACKET                                = 0xffff
	IP_MAX_MEMBERSHIPS                          = 0x14
	IP_MF                                       = 0x2000
	IP_MINTTL                                   = 0x15
	IP_MSFILTER                                 = 0x29
	IP_MSS                                      = 0x240
	IP_MTU                                      = 0xe
	IP_MTU_DISCOVER                             = 0xa
	IP_MULTICAST_ALL                            = 0x31
	IP_MULTICAST_IF                             = 0x20
	IP_MULTICAST_LOOP                           = 0x22
	IP_MULTICAST_TTL                            = 0x21
	IP_NODEFRAG                                 = 0x16
	IP_OFFMASK                                  = 0x1fff
	IP_OPTIONS                                  = 0x4
	IP_ORIGDSTADDR                              = 0x14
	IP_PASSSEC                                  = 0x12
	IP_PKTINFO                                  = 0x8
	IP_PKTOPTIONS                               = 0x9
	IP_PMTUDISC                                 = 0xa
	IP_PMTUDISC_DO                              = 0x2
	IP_PMTUDISC_DONT                            = 0x0
	IP_PMTUDISC_INTERFACE                       = 0x4
	IP_PMTUDISC_OMIT                            = 0x5
	IP_PMTUDISC_PROBE                           = 0x3
	IP_PMTUDISC_WANT                            = 0x1
	IP_PROTOCOL                                 = 0x34
	IP_RECVERR                                  = 0xb
	IP_RECVERR_RFC4884                          = 0x1a
	IP_RECVFRAGSIZE                             = 0x19
	IP_RECVOPTS                                 = 0x6
	IP_RECVORIGDSTADDR                          = 0x14
	IP_RECVRETOPTS                              = 0x7
	IP_RECVTOS                                  = 0xd
	IP_RECVTTL                                  = 0xc
	IP_RETOPTS                                  = 0x7
	IP_RF                                       = 0x8000
	IP_ROUTER_ALERT                             = 0x5
	IP_TOS                                      = 0x1
	IP_TRANSPARENT                              = 0x13
	IP_TTL                                      = 0x2
	IP_UNBLOCK_SOURCE                           = 0x25
	IP_UNICAST_IF                               = 0x32
	IP_XFRM_POLICY                              = 0x11
	ISOFS_SUPER_MAGIC                           = 0x9660
	ISTRIP                                      = 0x20
	ITIMER_PROF                                 = 0x2
	ITIMER_REAL                                 = 0x0
	ITIMER_VIRTUAL                              = 0x1
	IUTF8                                       = 0x4000
	IXANY                                       = 0x800
	JFFS2_SUPER_MAGIC                           = 0x72b6
	KCMPROTO_CONNECTED                          = 0x0
	KCM_RECV_DISABLE                            = 0x1
	KEXEC_ARCH_386                              = 0x30000
	KEXEC_ARCH_68K                              = 0x40000
	KEXEC_ARCH_AARCH64                          = 0xb70000
	KEXEC_ARCH_ARM                              = 0x280000
	KEXEC_ARCH_DEFAULT                          = 0x0
	KEXEC_ARCH_IA_64                            = 0x320000
	KEXEC_ARCH_LOONGARCH                        = 0x1020000
	KEXEC_ARCH_MASK                             = 0xffff0000
	KEXEC_ARCH_MIPS                             = 0x80000
	KEXEC_ARCH_MIPS_LE                          = 0xa0000
	KEXEC_ARCH_PARISC                           = 0xf0000
	KEXEC_ARCH_PPC                              = 0x140000
	KEXEC_ARCH_PPC64                            = 0x150000
	KEXEC_ARCH_RISCV                            = 0xf30000
	KEXEC_ARCH_S390                             = 0x160000
	KEXEC_ARCH_SH                               = 0x2a0000
	KEXEC_ARCH_X86_64                           = 0x3e0000
	KEXEC_CRASH_HOTPLUG_SUPPORT                 = 0x8
	KEXEC_FILE_DEBUG                            = 0x8
	KEXEC_FILE_NO_INITRAMFS                     = 0x4
	KEXEC_FILE_ON_CRASH                         = 0x2
	KEXEC_FILE_UNLOAD                           = 0x1
	KEXEC_ON_CRASH                              = 0x1
	KEXEC_PRESERVE_CONTEXT                      = 0x2
	KEXEC_SEGMENT_MAX                           = 0x10
	KEXEC_UPDATE_ELFCOREHDR                     = 0x4
	KEYCTL_ASSUME_AUTHORITY                     = 0x10
	KEYCTL_CAPABILITIES                         = 0x1f
	KEYCTL_CAPS0_BIG_KEY                        = 0x10
	KEYCTL_CAPS0_CAPABILITIES                   = 0x1
	KEYCTL_CAPS0_DIFFIE_HELLMAN                 = 0x4
	KEYCTL_CAPS0_INVALIDATE                     = 0x20
	KEYCTL_CAPS0_MOVE                           = 0x80
	KEYCTL_CAPS0_PERSISTENT_KEYRINGS            = 0x2
	KEYCTL_CAPS0_PUBLIC_KEY                     = 0x8
	KEYCTL_CAPS0_RESTRICT_KEYRING               = 0x40
	KEYCTL_CAPS1_NOTIFICATIONS                  = 0x4
	KEYCTL_CAPS1_NS_KEYRING_NAME                = 0x1
	KEYCTL_CAPS1_NS_KEY_TAG                     = 0x2
	KEYCTL_CHOWN                                = 0x4
	KEYCTL_CLEAR                                = 0x7
	KEYCTL_DESCRIBE                             = 0x6
	KEYCTL_DH_COMPUTE                           = 0x17
	KEYCTL_GET_KEYRING_ID                       = 0x0
	KEYCTL_GET_PERSISTENT                       = 0x16
	KEYCTL_GET_SECURITY                         = 0x11
	KEYCTL_INSTANTIATE                          = 0xc
	KEYCTL_INSTANTIATE_IOV                      = 0x14
	KEYCTL_INVALIDATE                           = 0x15
	KEYCTL_JOIN_SESSION_KEYRING                 = 0x1
	KEYCTL_LINK                                 = 0x8
	KEYCTL_MOVE                                 = 0x1e
	KEYCTL_MOVE_EXCL                            = 0x1
	KEYCTL_NEGATE                               = 0xd
	KEYCTL_PKEY_DECRYPT                         = 0x1a
	KEYCTL_PKEY_ENCRYPT                         = 0x19
	KEYCTL_PKEY_QUERY                           = 0x18
	KEYCTL_PKEY_SIGN                            = 0x1b
	KEYCTL_PKEY_VERIFY                          = 0x1c
	KEYCTL_READ                                 = 0xb
	KEYCTL_REJECT                               = 0x13
	KEYCTL_RESTRICT_KEYRING                     = 0x1d
	KEYCTL_REVOKE                               = 0x3
	KEYCTL_SEARCH                               = 0xa
	KEYCTL_SESSION_TO_PARENT                    = 0x12
	KEYCTL_SETPERM                              = 0x5
	KEYCTL_SET_REQKEY_KEYRING                   = 0xe
	KEYCTL_SET_TIMEOUT                          = 0xf
	KEYCTL_SUPPORTS_DECRYPT                     = 0x2
	KEYCTL_SUPPORTS_ENCRYPT                     = 0x1
	KEYCTL_SUPPORTS_SIGN                        = 0x4
	KEYCTL_SUPPORTS_VERIFY                      = 0x8
	KEYCTL_UNLINK                               = 0x9
	KEYCTL_UPDATE                               = 0x2
	KEYCTL_WATCH_KEY                            = 0x20
	KEY_REQKEY_DEFL_DEFAULT                     = 0x0
	KEY_REQKEY_DEFL_GROUP_KEYRING               = 0x6
	KEY_REQKEY_DEFL_NO_CHANGE                   = -0x1
	KEY_REQKEY_DEFL_PROCESS_KEYRING             = 0x2
	KEY_REQKEY_DEFL_REQUESTOR_KEYRING           = 0x7
	KEY_REQKEY_DEFL_SESSION_KEYRING             = 0x3
	KEY_REQKEY_DEFL_THREAD_KEYRING              = 0x1
	KEY_REQKEY_DEFL_USER_KEYRING                = 0x4
	KEY_REQKEY_DEFL_USER_SESSION_KEYRING        = 0x5
	KEY_SPEC_GROUP_KEYRING                      = -0x6
	KEY_SPEC_PROCESS_KEYRING                    = -0x2
	KEY_SPEC_REQKEY_AUTH_KEY                    = -0x7
	KEY_SPEC_REQUESTOR_KEYRING                  = -0x8
	KEY_SPEC_SESSION_KEYRING                    = -0x3
	KEY_SPEC_THREAD_KEYRING                     = -0x1
	KEY_SPEC_USER_KEYRING                       = -0x4
	KEY_SPEC_USER_SESSION_KEYRING               = -0x5
	LANDLOCK_ACCESS_FS_EXECUTE                  = 0x1
	LANDLOCK_ACCESS_FS_IOCTL_DEV                = 0x8000
	LANDLOCK_ACCESS_FS_MAKE_BLOCK               = 0x800
	LANDLOCK_ACCESS_FS_MAKE_CHAR                = 0x40
	LANDLOCK_ACCESS_FS_MAKE_DIR                 = 0x80
	LANDLOCK_ACCESS_FS_MAKE_FIFO                = 0x400
	LANDLOCK_ACCESS_FS_MAKE_REG                 = 0x100
	LANDLOCK_ACCESS_FS_MAKE_SOCK                = 0x200
	LANDLOCK_ACCESS_FS_MAKE_SYM                 = 0x1000
	LANDLOCK_ACCESS_FS_READ_DIR                 = 0x8
	LANDLOCK_ACCESS_FS_READ_FILE                = 0x4
	LANDLOCK_ACCESS_FS_REFER                    = 0x2000
	LANDLOCK_ACCESS_FS_REMOVE_DIR               = 0x10
	LANDLOCK_ACCESS_FS_REMOVE_FILE              = 0x20
	LANDLOCK_ACCESS_FS_TRUNCATE                 = 0x4000
	LANDLOCK_ACCESS_FS_WRITE_FILE               = 0x2
	LANDLOCK_ACCESS_NET_BIND_TCP                = 0x1
	LANDLOCK_ACCESS_NET_CONNECT_TCP             = 0x2
	LANDLOCK_CREATE_RULESET_ERRATA              = 0x2
	LANDLOCK_CREATE_RULESET_VERSION             = 0x1
	LANDLOCK_RESTRICT_SELF_LOG_NEW_EXEC_ON      = 0x2
	LANDLOCK_RESTRICT_SELF_LOG_SAME_EXEC_OFF    = 0x1
	LANDLOCK_RESTRICT_SELF_LOG_SUBDOMAINS_OFF   = 0x4
	LANDLOCK_SCOPE_ABSTRACT_UNIX_SOCKET         = 0x1
	LANDLOCK_SCOPE_SIGNAL                       = 0x2
	LINUX_REBOOT_CMD_CAD_OFF                    = 0x0
	LINUX_REBOOT_CMD_CAD_ON                     = 0x89abcdef
	LINUX_REBOOT_CMD_HALT                       = 0xcdef0123
	LINUX_REBOOT_CMD_KEXEC                      = 0x45584543
	LINUX_REBOOT_CMD_POWER_OFF                  = 0x4321fedc
	LINUX_REBOOT_CMD_RESTART                    = 0x1234567
	LINUX_REBOOT_CMD_RESTART2                   = 0xa1b2c3d4
	LINUX_REBOOT_CMD_SW_SUSPEND                 = 0xd000fce2
	LINUX_REBOOT_MAGIC1                         = 0xfee1dead
	LINUX_REBOOT_MAGIC2                         = 0x28121969
	LOCK_EX                                     = 0x2
	LOCK_NB                                     = 0x4
	LOCK_SH                                     = 0x1
	LOCK_UN                                     = 0x8
	LOOP_CLR_FD                                 = 0x4c01
	LOOP_CONFIGURE                              = 0x4c0a
	LOOP_CTL_ADD                                = 0x4c80
	LOOP_CTL_GET_FREE                           = 0x4c82
	LOOP_CTL_REMOVE                             = 0x4c81
	LOOP_GET_STATUS                             = 0x4c03
	LOOP_GET_STATUS64                           = 0x4c05
	LOOP_SET_BLOCK_SIZE                         = 0x4c09
	LOOP_SET_CAPACITY                           = 0x4c07
	LOOP_SET_DIRECT_IO                          = 0x4c08
	LOOP_SET_FD                                 = 0x4c00
	LOOP_SET_STATUS                             = 0x4c02
	LOOP_SET_STATUS64                           = 0x4c04
	LOOP_SET_STATUS_CLEARABLE_FLAGS             = 0x4
	LOOP_SET_STATUS_SETTABLE_FLAGS              = 0xc
	LO_KEY_SIZE                                 = 0x20
	LO_NAME_SIZE                                = 0x40
	LWTUNNEL_IP6_MAX                            = 0x8
	LWTUNNEL_IP_MAX                             = 0x8
	LWTUNNEL_IP_OPTS_MAX                        = 0x3
	LWTUNNEL_IP_OPT_ERSPAN_MAX                  = 0x4
	LWTUNNEL_IP_OPT_GENEVE_MAX                  = 0x3
	LWTUNNEL_IP_OPT_VXLAN_MAX                   = 0x1
	MADV_COLD                                   = 0x14
	MADV_COLLAPSE                               = 0x19
	MADV_DODUMP                                 = 0x11
	MADV_DOFORK                                 = 0xb
	MADV_DONTDUMP                               = 0x10
	MADV_DONTFORK                               = 0xa
	MADV_DONTNEED                               = 0x4
	MADV_DONTNEED_LOCKED                        = 0x18
	MADV_FREE                                   = 0x8
	MADV_HUGEPAGE                               = 0xe
	MADV_HWPOISON                               = 0x64
	MADV_KEEPONFORK                             = 0x13
	MADV_MERGEABLE                              = 0xc
	MADV_NOHUGEPAGE                             = 0xf
	MADV_NORMAL                                 = 0x0
	MADV_PAGEOUT                                = 0x15
	MADV_POPULATE_READ                          = 0x16
	MADV_POPULATE_WRITE                         = 0x17
	MADV_RANDOM                                 = 0x1
	MADV_REMOVE                                 = 0x9
	MADV_SEQUENTIAL                             = 0x2
	MADV_UNMERGEABLE                            = 0xd
	MADV_WILLNEED                               = 0x3
	MADV_WIPEONFORK                             = 0x12
	MAP_DROPPABLE                               = 0x8
	MAP_FILE                                    = 0x0
	MAP_FIXED                                   = 0x10
	MAP_FIXED_NOREPLACE                         = 0x100000
	MAP_HUGE_16GB                               = 0x88000000
	MAP_HUGE_16KB                               = 0x38000000
	MAP_HUGE_16MB                               = 0x60000000
	MAP_HUGE_1GB                                = 0x78000000
	MAP_HUGE_1MB                                = 0x50000000
	MAP_HUGE_256MB                              = 0x70000000
	MAP_HUGE_2GB                                = 0x7c000000
	MAP_HUGE_2MB                                = 0x54000000
	MAP_HUGE_32MB                               = 0x64000000
	MAP_HUGE_512KB                              = 0x4c000000
	MAP_HUGE_512MB                              = 0x74000000
	MAP_HUGE_64KB                               = 0x40000000
	MAP_HUGE_8MB                                = 0x5c000000
	MAP_HUGE_MASK                               = 0x3f
	MAP_HUGE_SHIFT                              = 0x1a
	MAP_PRIVATE                                 = 0x2
	MAP_SHARED                                  = 0x1
	MAP_SHARED_VALIDATE                         = 0x3
	MAP_TYPE                                    = 0xf
	MCAST_BLOCK_SOURCE                          = 0x2b
	MCAST_EXCLUDE                               = 0x0
	MCAST_INCLUDE                               = 0x1
	MCAST_JOIN_GROUP                            = 0x2a
	MCAST_JOIN_SOURCE_GROUP                     = 0x2e
	MCAST_LEAVE_GROUP                           = 0x2d
	MCAST_LEAVE_SOURCE_GROUP                    = 0x2f
	MCAST_MSFILTER                              = 0x30
	MCAST_UNBLOCK_SOURCE                        = 0x2c
	MEMGETREGIONINFO                            = 0xc0104d08
	MEMREADOOB64                                = 0xc0184d16
	MEMWRITE                                    = 0xc0304d18
	MEMWRITEOOB64                               = 0xc0184d15
	MFD_ALLOW_SEALING                           = 0x2
	MFD_CLOEXEC                                 = 0x1
	MFD_EXEC                                    = 0x10
	MFD_HUGETLB                                 = 0x4
	MFD_HUGE_16GB                               = 0x88000000
	MFD_HUGE_16MB                               = 0x60000000
	MFD_HUGE_1GB                                = 0x78000000
	MFD_HUGE_1MB                                = 0x50000000
	MFD_HUGE_256MB                              = 0x70000000
	MFD_HUGE_2GB                                = 0x7c000000
	MFD_HUGE_2MB                                = 0x54000000
	MFD_HUGE_32MB                               = 0x64000000
	MFD_HUGE_512KB                              = 0x4c000000
	MFD_HUGE_512MB                              = 0x74000000
	MFD_HUGE_64KB                               = 0x40000000
	MFD_HUGE_8MB                                = 0x5c000000
	MFD_HUGE_MASK                               = 0x3f
	MFD_HUGE_SHIFT                              = 0x1a
	MFD_NOEXEC_SEAL                             = 0x8
	MINIX2_SUPER_MAGIC                          = 0x2468
	MINIX2_SUPER_MAGIC2                         = 0x2478
	MINIX3_SUPER_MAGIC                          = 0x4d5a
	MINIX_SUPER_MAGIC                           = 0x137f
	MINIX_SUPER_MAGIC2                          = 0x138f
	MNT_DETACH                                  = 0x2
	MNT_EXPIRE                                  = 0x4
	MNT_FORCE                                   = 0x1
	MNT_ID_REQ_SIZE_VER0                        = 0x18
	MNT_ID_REQ_SIZE_VER1                        = 0x20
	MNT_NS_INFO_SIZE_VER0                       = 0x10
	MODULE_INIT_COMPRESSED_FILE                 = 0x4
	MODULE_INIT_IGNORE_MODVERSIONS              = 0x1
	MODULE_INIT_IGNORE_VERMAGIC                 = 0x2
	MOUNT_ATTR_IDMAP                            = 0x100000
	MOUNT_ATTR_NOATIME                          = 0x10
	MOUNT_ATTR_NODEV                            = 0x4
	MOUNT_ATTR_NODIRATIME                       = 0x80
	MOUNT_ATTR_NOEXEC                           = 0x8
	MOUNT_ATTR_NOSUID                           = 0x2
	MOUNT_ATTR_NOSYMFOLLOW                      = 0x200000
	MOUNT_ATTR_RDONLY                           = 0x1
	MOUNT_ATTR_RELATIME                         = 0x0
	MOUNT_ATTR_SIZE_VER0                        = 0x20
	MOUNT_ATTR_STRICTATIME                      = 0x20
	MOUNT_ATTR__ATIME                           = 0x70
	MREMAP_DONTUNMAP                            = 0x4
	MREMAP_FIXED                                = 0x2
	MREMAP_MAYMOVE                              = 0x1
	MSDOS_SUPER_MAGIC                           = 0x4d44
	MSG_BATCH                                   = 0x40000
	MSG_CMSG_CLOEXEC                            = 0x40000000
	MSG_CONFIRM                                 = 0x800
	MSG_CTRUNC                                  = 0x8
	MSG_DONTROUTE                               = 0x4
	MSG_DONTWAIT                                = 0x40
	MSG_EOR                                     = 0x80
	MSG_ERRQUEUE                                = 0x2000
	MSG_FASTOPEN                                = 0x20000000
	MSG_FIN                                     = 0x200
	MSG_MORE                                    = 0x8000
	MSG_NOSIGNAL                                = 0x4000
	MSG_OOB                                     = 0x1
	MSG_PEEK                                    = 0x2
	MSG_PROXY                                   = 0x10
	MSG_RST                                     = 0x1000
	MSG_SOCK_DEVMEM                             = 0x2000000
	MSG_SYN                                     = 0x400
	MSG_TRUNC                                   = 0x20
	MSG_TRYHARD                                 = 0x4
	MSG_WAITALL                                 = 0x100
	MSG_WAITFORONE                              = 0x10000
	MSG_ZEROCOPY                                = 0x4000000
	MS_ACTIVE                                   = 0x40000000
	MS_ASYNC                                    = 0x1
	MS_BIND                                     = 0x1000
	MS_BORN                                     = 0x20000000
	MS_DIRSYNC                                  = 0x80
	MS_INVALIDATE                               = 0x2
	MS_I_VERSION                                = 0x800000
	MS_KERNMOUNT                                = 0x400000
	MS_LAZYTIME                                 = 0x2000000
	MS_MANDLOCK                                 = 0x40
	MS_MGC_MSK                                  = 0xffff0000
	MS_MGC_VAL                                  = 0xc0ed0000
	MS_MOVE                                     = 0x2000
	MS_NOATIME                                  = 0x400
	MS_NODEV                                    = 0x4
	MS_NODIRATIME                               = 0x800
	MS_NOEXEC                                   = 0x8
	MS_NOREMOTELOCK                             = 0x8000000
	MS_NOSEC                                    = 0x10000000
	MS_NOSUID                                   = 0x2
	MS_NOSYMFOLLOW                              = 0x100
	MS_NOUSER                                   = -0x80000000
	MS_POSIXACL                                 = 0x10000
	MS_PRIVATE                                  = 0x40000
	MS_RDONLY                                   = 0x1
	MS_REC                                      = 0x4000
	MS_RELATIME                                 = 0x200000
	MS_REMOUNT                                  = 0x20
	MS_RMT_MASK                                 = 0x2800051
	MS_SHARED                                   = 0x100000
	MS_SILENT                                   = 0x8000
	MS_SLAVE                                    = 0x80000
	MS_STRICTATIME                              = 0x1000000
	MS_SUBMOUNT                                 = 0x4000000
	MS_SYNC                                     = 0x4
	MS_SYNCHRONOUS                              = 0x10
	MS_UNBINDABLE                               = 0x20000
	MS_VERBOSE                                  = 0x8000
	MTD_ABSENT                                  = 0x0
	MTD_BIT_WRITEABLE                           = 0x800
	MTD_CAP_NANDFLASH                           = 0x400
	MTD_CAP_NORFLASH                            = 0xc00
	MTD_CAP_NVRAM                               = 0x1c00
	MTD_CAP_RAM                                 = 0x1c00
	MTD_CAP_ROM                                 = 0x0
	MTD_DATAFLASH                               = 0x6
	MTD_INODE_FS_MAGIC                          = 0x11307854
	MTD_MAX_ECCPOS_ENTRIES                      = 0x40
	MTD_MAX_OOBFREE_ENTRIES                     = 0x8
	MTD_MLCNANDFLASH                            = 0x8
	MTD_NANDECC_AUTOPLACE                       = 0x2
	MTD_NANDECC_AUTOPL_USR                      = 0x4
	MTD_NANDECC_OFF                             = 0x0
	MTD_NANDECC_PLACE                           = 0x1
	MTD_NANDECC_PLACEONLY                       = 0x3
	MTD_NANDFLASH                               = 0x4
	MTD_NORFLASH                                = 0x3
	MTD_NO_ERASE                                = 0x1000
	MTD_OTP_FACTORY                             = 0x1
	MTD_OTP_OFF                                 = 0x0
	MTD_OTP_USER                                = 0x2
	MTD_POWERUP_LOCK                            = 0x2000
	MTD_RAM                                     = 0x1
	MTD_ROM                                     = 0x2
	MTD_SLC_ON_MLC_EMULATION                    = 0x4000
	MTD_UBIVOLUME                               = 0x7
	MTD_WRITEABLE                               = 0x400
	NAME_MAX                                    = 0xff
	NCP_SUPER_MAGIC                             = 0x564c
	NETLINK_ADD_MEMBERSHIP                      = 0x1
	NETLINK_AUDIT                               = 0x9
	NETLINK_BROADCAST_ERROR                     = 0x4
	NETLINK_CAP_ACK                             = 0xa
	NETLINK_CONNECTOR                           = 0xb
	NETLINK_CRYPTO                              = 0x15
	NETLINK_DNRTMSG                             = 0xe
	NETLINK_DROP_MEMBERSHIP                     = 0x2
	NETLINK_ECRYPTFS                            = 0x13
	NETLINK_EXT_ACK                             = 0xb
	NETLINK_FIB_LOOKUP                          = 0xa
	NETLINK_FIREWALL                            = 0x3
	NETLINK_GENERIC                             = 0x10
	NETLINK_GET_STRICT_CHK                      = 0xc
	NETLINK_INET_DIAG                           = 0x4
	NETLINK_IP6_FW                              = 0xd
	NETLINK_ISCSI                               = 0x8
	NETLINK_KOBJECT_UEVENT                      = 0xf
	NETLINK_LISTEN_ALL_NSID                     = 0x8
	NETLINK_LIST_MEMBERSHIPS                    = 0x9
	NETLINK_NETFILTER                           = 0xc
	NETLINK_NFLOG                               = 0x5
	NETLINK_NO_ENOBUFS                          = 0x5
	NETLINK_PKTINFO                             = 0x3
	NETLINK_RDMA                                = 0x14
	NETLINK_ROUTE                               = 0x0
	NETLINK_RX_RING                             = 0x6
	NETLINK_SCSITRANSPORT                       = 0x12
	NETLINK_SELINUX                             = 0x7
	NETLINK_SMC                                 = 0x16
	NETLINK_SOCK_DIAG                           = 0x4
	NETLINK_TX_RING                             = 0x7
	NETLINK_UNUSED                              = 0x1
	NETLINK_USERSOCK                            = 0x2
	NETLINK_XFRM                                = 0x6
	NETNSA_MAX                                  = 0x5
	NETNSA_NSID_NOT_ASSIGNED                    = -0x1
	NFC_ATR_REQ_GB_MAXSIZE                      = 0x30
	NFC_ATR_REQ_MAXSIZE                         = 0x40
	NFC_ATR_RES_GB_MAXSIZE                      = 0x2f
	NFC_ATR_RES_MAXSIZE                         = 0x40
	NFC_ATS_MAXSIZE                             = 0x14
	NFC_COMM_ACTIVE                             = 0x0
	NFC_COMM_PASSIVE                            = 0x1
	NFC_DEVICE_NAME_MAXSIZE                     = 0x8
	NFC_DIRECTION_RX                            = 0x0
	NFC_DIRECTION_TX                            = 0x1
	NFC_FIRMWARE_NAME_MAXSIZE                   = 0x20
	NFC_GB_MAXSIZE                              = 0x30
	NFC_GENL_MCAST_EVENT_NAME                   = "events"
	NFC_GENL_NAME                               = "nfc"
	NFC_GENL_VERSION                            = 0x1
	NFC_HEADER_SIZE                             = 0x1
	NFC_ISO15693_UID_MAXSIZE                    = 0x8
	NFC_LLCP_MAX_SERVICE_NAME                   = 0x3f
	NFC_LLCP_MIUX                               = 0x1
	NFC_LLCP_REMOTE_LTO                         = 0x3
	NFC_LLCP_REMOTE_MIU                         = 0x2
	NFC_LLCP_REMOTE_RW                          = 0x4
	NFC_LLCP_RW                                 = 0x0
	NFC_NFCID1_MAXSIZE                          = 0xa
	NFC_NFCID2_MAXSIZE                          = 0x8
	NFC_NFCID3_MAXSIZE                          = 0xa
	NFC_PROTO_FELICA                            = 0x3
	NFC_PROTO_FELICA_MASK                       = 0x8
	NFC_PROTO_ISO14443                          = 0x4
	NFC_PROTO_ISO14443_B                        = 0x6
	NFC_PROTO_ISO14443_B_MASK                   = 0x40
	NFC_PROTO_ISO14443_MASK                     = 0x10
	NFC_PROTO_ISO15693                          = 0x7
	NFC_PROTO_ISO15693_MASK                     = 0x80
	NFC_PROTO_JEWEL                             = 0x1
	NFC_PROTO_JEWEL_MASK                        = 0x2
	NFC_PROTO_MAX                               = 0x8
	NFC_PROTO_MIFARE                            = 0x2
	NFC_PROTO_MIFARE_MASK                       = 0x4
	NFC_PROTO_NFC_DEP                           = 0x5
	NFC_PROTO_NFC_DEP_MASK                      = 0x20
	NFC_RAW_HEADER_SIZE                         = 0x2
	NFC_RF_INITIATOR                            = 0x0
	NFC_RF_NONE                                 = 0x2
	NFC_RF_TARGET                               = 0x1
	NFC_SENSB_RES_MAXSIZE                       = 0xc
	NFC_SENSF_RES_MAXSIZE                       = 0x12
	NFC_SE_DISABLED                             = 0x0
	NFC_SE_EMBEDDED                             = 0x2
	NFC_SE_ENABLED                              = 0x1
	NFC_SE_UICC                                 = 0x1
	NFC_SOCKPROTO_LLCP                          = 0x1
	NFC_SOCKPROTO_MAX                           = 0x2
	NFC_SOCKPROTO_RAW                           = 0x0
	NFNETLINK_V0                                = 0x0
	NFNLGRP_ACCT_QUOTA                          = 0x8
	NFNLGRP_CONNTRACK_DESTROY                   = 0x3
	NFNLGRP_CONNTRACK_EXP_DESTROY               = 0x6
	NFNLGRP_CONNTRACK_EXP_NEW                   = 0x4
	NFNLGRP_CONNTRACK_EXP_UPDATE                = 0x5
	NFNLGRP_CONNTRACK_NEW                       = 0x1
	NFNLGRP_CONNTRACK_UPDATE                    = 0x2
	NFNLGRP_MAX                                 = 0x9
	NFNLGRP_NFTABLES                            = 0x7
	NFNLGRP_NFTRACE                             = 0x9
	NFNLGRP_NONE                                = 0x0
	NFNL_BATCH_MAX                              = 0x1
	NFNL_MSG_BATCH_BEGIN                        = 0x10
	NFNL_MSG_BATCH_END                          = 0x11
	NFNL_NFA_NEST                               = 0x8000
	NFNL_SUBSYS_ACCT                            = 0x7
	NFNL_SUBSYS_COUNT                           = 0xd
	NFNL_SUBSYS_CTHELPER                        = 0x9
	NFNL_SUBSYS_CTNETLINK                       = 0x1
	NFNL_SUBSYS_CTNETLINK_EXP                   = 0x2
	NFNL_SUBSYS_CTNETLINK_TIMEOUT               = 0x8
	NFNL_SUBSYS_HOOK                            = 0xc
	NFNL_SUBSYS_IPSET                           = 0x6
	NFNL_SUBSYS_NFTABLES                        = 0xa
	NFNL_SUBSYS_NFT_COMPAT                      = 0xb
	NFNL_SUBSYS_NONE                            = 0x0
	NFNL_SUBSYS_OSF                             = 0x5
	NFNL_SUBSYS_QUEUE                           = 0x3
	NFNL_SUBSYS_ULOG                            = 0x4
	NFS_SUPER_MAGIC                             = 0x6969
	NFT_BITWISE_BOOL                            = 0x0
	NFT_CHAIN_FLAGS                             = 0x7
	NFT_CHAIN_MAXNAMELEN                        = 0x100
	NFT_CT_MAX                                  = 0x17
	NFT_DATA_RESERVED_MASK                      = 0xffffff00
	NFT_DATA_VALUE_MAXLEN                       = 0x40
	NFT_EXTHDR_OP_MAX                           = 0x4
	NFT_FIB_RESULT_MAX                          = 0x3
	NFT_INNER_MASK                              = 0xf
	NFT_LOGLEVEL_MAX                            = 0x8
	NFT_NAME_MAXLEN                             = 0x100
	NFT_NG_MAX                                  = 0x1
	NFT_OBJECT_CONNLIMIT                        = 0x5
	NFT_OBJECT_COUNTER                          = 0x1
	NFT_OBJECT_CT_EXPECT                        = 0x9
	NFT_OBJECT_CT_HELPER                        = 0x3
	NFT_OBJECT_CT_TIMEOUT                       = 0x7
	NFT_OBJECT_LIMIT                            = 0x4
	NFT_OBJECT_MAX                              = 0xa
	NFT_OBJECT_QUOTA                            = 0x2
	NFT_OBJECT_SECMARK                          = 0x8
	NFT_OBJECT_SYNPROXY                         = 0xa
	NFT_OBJECT_TUNNEL                           = 0x6
	NFT_OBJECT_UNSPEC                           = 0x0
	NFT_OBJ_MAXNAMELEN                          = 0x100
	NFT_OSF_MAXGENRELEN                         = 0x10
	NFT_QUEUE_FLAG_BYPASS                       = 0x1
	NFT_QUEUE_FLAG_CPU_FANOUT                   = 0x2
	NFT_QUEUE_FLAG_MASK                         = 0x3
	NFT_REG32_COUNT                             = 0x10
	NFT_REG32_SIZE                              = 0x4
	NFT_REG_MAX                                 = 0x4
	NFT_REG_SIZE                                = 0x10
	NFT_REJECT_ICMPX_MAX                        = 0x3
	NFT_RT_MAX                                  = 0x4
	NFT_SECMARK_CTX_MAXLEN                      = 0x1000
	NFT_SET_MAXNAMELEN                          = 0x100
	NFT_SOCKET_MAX                              = 0x3
	NFT_TABLE_F_MASK                            = 0x7
	NFT_TABLE_MAXNAMELEN                        = 0x100
	NFT_TRACETYPE_MAX                           = 0x3
	NFT_TUNNEL_F_MASK                           = 0x7
	NFT_TUNNEL_MAX                              = 0x1
	NFT_TUNNEL_MODE_MAX                         = 0x2
	NFT_USERDATA_MAXLEN                         = 0x100
	NFT_XFRM_KEY_MAX                            = 0x6
	NF_NAT_RANGE_MAP_IPS                        = 0x1
	NF_NAT_RANGE_MASK                           = 0x7f
	NF_NAT_RANGE_NETMAP                         = 0x40
	NF_NAT_RANGE_PERSISTENT                     = 0x8
	NF_NAT_RANGE_PROTO_OFFSET                   = 0x20
	NF_NAT_RANGE_PROTO_RANDOM                   = 0x4
	NF_NAT_RANGE_PROTO_RANDOM_ALL               = 0x14
	NF_NAT_RANGE_PROTO_RANDOM_FULLY             = 0x10
	NF_NAT_RANGE_PROTO_SPECIFIED                = 0x2
	NILFS_SUPER_MAGIC                           = 0x3434
	NL0                                         = 0x0
	NL1                                         = 0x100
	NLA_ALIGNTO                                 = 0x4
	NLA_F_NESTED                                = 0x8000
	NLA_F_NET_BYTEORDER                         = 0x4000
	NLA_HDRLEN                                  = 0x4
	NLMSG_ALIGNTO                               = 0x4
	NLMSG_DONE                                  = 0x3
	NLMSG_ERROR                                 = 0x2
	NLMSG_HDRLEN                                = 0x10
	NLMSG_MIN_TYPE                              = 0x10
	NLMSG_NOOP                                  = 0x1
	NLMSG_OVERRUN                               = 0x4
	NLM_F_ACK                                   = 0x4
	NLM_F_ACK_TLVS                              = 0x200
	NLM_F_APPEND                                = 0x800
	NLM_F_ATOMIC                                = 0x400
	NLM_F_BULK                                  = 0x200
	NLM_F_CAPPED                                = 0x100
	NLM_F_CREATE                                = 0x400
	NLM_F_DUMP                                  = 0x300
	NLM_F_DUMP_FILTERED                         = 0x20
	NLM_F_DUMP_INTR                             = 0x10
	NLM_F_ECHO                                  = 0x8
	NLM_F_EXCL                                  = 0x200
	NLM_F_MATCH                                 = 0x200
	NLM_F_MULTI                                 = 0x2
	NLM_F_NONREC                                = 0x100
	NLM_F_REPLACE                               = 0x100
	NLM_F_REQUEST                               = 0x1
	NLM_F_ROOT                                  = 0x100
	NSFS_MAGIC                                  = 0x6e736673
	OCFS2_SUPER_MAGIC                           = 0x7461636f
	OCRNL                                       = 0x8
	OFDEL                                       = 0x80
	OFILL                                       = 0x40
	ONLRET                                      = 0x20
	ONOCR                                       = 0x10
	OPENPROM_SUPER_MAGIC                        = 0x9fa1
	OPOST                                       = 0x1
	OVERLAYFS_SUPER_MAGIC                       = 0x794c7630
	O_ACCMODE                                   = 0x3
	O_RDONLY                                    = 0x0
	O_RDWR                                      = 0x2
	O_WRONLY                                    = 0x1
	PACKET_ADD_MEMBERSHIP                       = 0x1
	PACKET_AUXDATA                              = 0x8
	PACKET_BROADCAST                            = 0x1
	PACKET_COPY_THRESH                          = 0x7
	PACKET_DROP_MEMBERSHIP                      = 0x2
	PACKET_FANOUT                               = 0x12
	PACKET_FANOUT_CBPF                          = 0x6
	PACKET_FANOUT_CPU                           = 0x2
	PACKET_FANOUT_DATA                          = 0x16
	PACKET_FANOUT_EBPF                          = 0x7
	PACKET_FANOUT_FLAG_DEFRAG                   = 0x8000
	PACKET_FANOUT_FLAG_IGNORE_OUTGOING          = 0x4000
	PACKET_FANOUT_FLAG_ROLLOVER                 = 0x1000
	PACKET_FANOUT_FLAG_UNIQUEID                 = 0x2000
	PACKET_FANOUT_HASH                          = 0x0
	PACKET_FANOUT_LB                            = 0x1
	PACKET_FANOUT_QM                            = 0x5
	PACKET_FANOUT_RND                           = 0x4
	PACKET_FANOUT_ROLLOVER                      = 0x3
	PACKET_FASTROUTE                            = 0x6
	PACKET_HDRLEN                               = 0xb
	PACKET_HOST                                 = 0x0
	PACKET_IGNORE_OUTGOING                      = 0x17
	PACKET_KERNEL                               = 0x7
	PACKET_LOOPBACK                             = 0x5
	PACKET_LOSS                                 = 0xe
	PACKET_MR_ALLMULTI                          = 0x2
	PACKET_MR_MULTICAST                         = 0x0
	PACKET_MR_PROMISC                           = 0x1
	PACKET_MR_UNICAST                           = 0x3
	PACKET_MULTICAST                            = 0x2
	PACKET_ORIGDEV                              = 0x9
	PACKET_OTHERHOST                            = 0x3
	PACKET_OUTGOING                             = 0x4
	PACKET_QDISC_BYPASS                         = 0x14
	PACKET_RECV_OUTPUT                          = 0x3
	PACKET_RESERVE                              = 0xc
	PACKET_ROLLOVER_STATS                       = 0x15
	PACKET_RX_RING                              = 0x5
	PACKET_STATISTICS                           = 0x6
	PACKET_TIMESTAMP                            = 0x11
	PACKET_TX_HAS_OFF                           = 0x13
	PACKET_TX_RING                              = 0xd
	PACKET_TX_TIMESTAMP                         = 0x10
	PACKET_USER                                 = 0x6
	PACKET_VERSION                              = 0xa
	PACKET_VNET_HDR                             = 0xf
	PACKET_VNET_HDR_SZ                          = 0x18
	PARITY_CRC16_PR0                            = 0x2
	PARITY_CRC16_PR0_CCITT                      = 0x4
	PARITY_CRC16_PR1                            = 0x3
	PARITY_CRC16_PR1_CCITT                      = 0x5
	PARITY_CRC32_PR0_CCITT                      = 0x6
	PARITY_CRC32_PR1_CCITT                      = 0x7
	PARITY_DEFAULT                              = 0x0
	PARITY_NONE                                 = 0x1
	PARMRK                                      = 0x8
	PERF_ATTR_SIZE_VER0                         = 0x40
	PERF_ATTR_SIZE_VER1                         = 0x48
	PERF_ATTR_SIZE_VER2                         = 0x50
	PERF_ATTR_SIZE_VER3                         = 0x60
	PERF_ATTR_SIZE_VER4                         = 0x68
	PERF_ATTR_SIZE_VER5                         = 0x70
	PERF_ATTR_SIZE_VER6                         = 0x78
	PERF_ATTR_SIZE_VER7                         = 0x80
	PERF_ATTR_SIZE_VER8                         = 0x88
	PERF_AUX_FLAG_COLLISION                     = 0x8
	PERF_AUX_FLAG_CORESIGHT_FORMAT_CORESIGHT    = 0x0
	PERF_AUX_FLAG_CORESIGHT_FORMAT_RAW          = 0x100
	PERF_AUX_FLAG_OVERWRITE                     = 0x2
	PERF_AUX_FLAG_PARTIAL                       = 0x4
	PERF_AUX_FLAG_PMU_FORMAT_TYPE_MASK          = 0xff00
	PERF_AUX_FLAG_TRUNCATED                     = 0x1
	PERF_BRANCH_ENTRY_INFO_BITS_MAX             = 0x21
	PERF_BR_ARM64_DEBUG_DATA                    = 0x7
	PERF_BR_ARM64_DEBUG_EXIT                    = 0x5
	PERF_BR_ARM64_DEBUG_HALT                    = 0x4
	PERF_BR_ARM64_DEBUG_INST                    = 0x6
	PERF_BR_ARM64_FIQ                           = 0x3
	PERF_FLAG_FD_CLOEXEC                        = 0x8
	PERF_FLAG_FD_NO_GROUP                       = 0x1
	PERF_FLAG_FD_OUTPUT                         = 0x2
	PERF_FLAG_PID_CGROUP                        = 0x4
	PERF_HW_EVENT_MASK                          = 0xffffffff
	PERF_MAX_CONTEXTS_PER_STACK                 = 0x8
	PERF_MAX_STACK_DEPTH                        = 0x7f
	PERF_MEM_BLK_ADDR                           = 0x4
	PERF_MEM_BLK_DATA                           = 0x2
	PERF_MEM_BLK_NA                             = 0x1
	PERF_MEM_BLK_SHIFT                          = 0x28
	PERF_MEM_HOPS_0                             = 0x1
	PERF_MEM_HOPS_1                             = 0x2
	PERF_MEM_HOPS_2                             = 0x3
	PERF_MEM_HOPS_3                             = 0x4
	PERF_MEM_HOPS_SHIFT                         = 0x2b
	PERF_MEM_LOCK_LOCKED                        = 0x2
	PERF_MEM_LOCK_NA                            = 0x1
	PERF_MEM_LOCK_SHIFT                         = 0x18
	PERF_MEM_LVLNUM_ANY_CACHE                   = 0xb
	PERF_MEM_LVLNUM_CXL                         = 0x9
	PERF_MEM_LVLNUM_IO                          = 0xa
	PERF_MEM_LVLNUM_L1                          = 0x1
	PERF_MEM_LVLNUM_L2                          = 0x2
	PERF_MEM_LVLNUM_L2_MHB                      = 0x5
	PERF_MEM_LVLNUM_L3                          = 0x3
	PERF_MEM_LVLNUM_L4                          = 0x4
	PERF_MEM_LVLNUM_LFB                         = 0xc
	PERF_MEM_LVLNUM_MSC                         = 0x6
	PERF_MEM_LVLNUM_NA                          = 0xf
	PERF_MEM_LVLNUM_PMEM                        = 0xe
	PERF_MEM_LVLNUM_RAM                         = 0xd
	PERF_MEM_LVLNUM_SHIFT                       = 0x21
	PERF_MEM_LVLNUM_UNC                         = 0x8
	PERF_MEM_LVL_HIT                            = 0x2
	PERF_MEM_LVL_IO                             = 0x1000
	PERF_MEM_LVL_L1                             = 0x8
	PERF_MEM_LVL_L2                             = 0x20
	PERF_MEM_LVL_L3                             = 0x40
	PERF_MEM_LVL_LFB                            = 0x10
	PERF_MEM_LVL_LOC_RAM                        = 0x80
	PERF_MEM_LVL_MISS                           = 0x4
	PERF_MEM_LVL_NA                             = 0x1
	PERF_MEM_LVL_REM_CCE1                       = 0x400
	PERF_MEM_LVL_REM_CCE2                       = 0x800
	PERF_MEM_LVL_REM_RAM1                       = 0x100
	PERF_MEM_LVL_REM_RAM2                       = 0x200
	PERF_MEM_LVL_SHIFT                          = 0x5
	PERF_MEM_LVL_UNC                            = 0x2000
	PERF_MEM_OP_EXEC                            = 0x10
	PERF_MEM_OP_LOAD                            = 0x2
	PERF_MEM_OP_NA                              = 0x1
	PERF_MEM_OP_PFETCH                          = 0x8
	PERF_MEM_OP_SHIFT                           = 0x0
	PERF_MEM_OP_STORE                           = 0x4
	PERF_MEM_REMOTE_REMOTE                      = 0x1
	PERF_MEM_REMOTE_SHIFT                       = 0x25
	PERF_MEM_SNOOPX_FWD                         = 0x1
	PERF_MEM_SNOOPX_PEER                        = 0x2
	PERF_MEM_SNOOPX_SHIFT                       = 0x26
	PERF_MEM_SNOOP_HIT                          = 0x4
	PERF_MEM_SNOOP_HITM                         = 0x10
	PERF_MEM_SNOOP_MISS                         = 0x8
	PERF_MEM_SNOOP_NA                           = 0x1
	PERF_MEM_SNOOP_NONE                         = 0x2
	PERF_MEM_SNOOP_SHIFT                        = 0x13
	PERF_MEM_TLB_HIT                            = 0x2
	PERF_MEM_TLB_L1                             = 0x8
	PERF_MEM_TLB_L2                             = 0x10
	PERF_MEM_TLB_MISS                           = 0x4
	PERF_MEM_TLB_NA                             = 0x1
	PERF_MEM_TLB_OS                             = 0x40
	PERF_MEM_TLB_SHIFT                          = 0x1a
	PERF_MEM_TLB_WK                             = 0x20
	PERF_PMU_TYPE_SHIFT                         = 0x20
	PERF_RECORD_KSYMBOL_FLAGS_UNREGISTER        = 0x1
	PERF_RECORD_MISC_COMM_EXEC                  = 0x2000
	PERF_RECORD_MISC_CPUMODE_MASK               = 0x7
	PERF_RECORD_MISC_CPUMODE_UNKNOWN            = 0x0
	PERF_RECORD_MISC_EXACT_IP                   = 0x4000
	PERF_RECORD_MISC_EXT_RESERVED               = 0x8000
	PERF_RECORD_MISC_FORK_EXEC                  = 0x2000
	PERF_RECORD_MISC_GUEST_KERNEL               = 0x4
	PERF_RECORD_MISC_GUEST_USER                 = 0x5
	PERF_RECORD_MISC_HYPERVISOR                 = 0x3
	PERF_RECORD_MISC_KERNEL                     = 0x1
	PERF_RECORD_MISC_MMAP_BUILD_ID              = 0x4000
	PERF_RECORD_MISC_MMAP_DATA                  = 0x2000
	PERF_RECORD_MISC_PROC_MAP_PARSE_TIMEOUT     = 0x1000
	PERF_RECORD_MISC_SWITCH_OUT                 = 0x2000
	PERF_RECORD_MISC_SWITCH_OUT_PREEMPT         = 0x4000
	PERF_RECORD_MISC_USER                       = 0x2
	PERF_SAMPLE_BRANCH_PLM_ALL                  = 0x7
	PERF_SAMPLE_WEIGHT_TYPE                     = 0x1004000
	PID_FS_MAGIC                                = 0x50494446
	PIPEFS_MAGIC                                = 0x50495045
	PPPIOCGNPMODE                               = 0xc008744c
	PPPIOCNEWUNIT                               = 0xc004743e
	PRIO_PGRP                                   = 0x1
	PRIO_PROCESS                                = 0x0
	PRIO_USER                                   = 0x2
	PROCFS_IOCTL_MAGIC                          = 'f'
	PROC_SUPER_MAGIC                            = 0x9fa0
	PROT_EXEC                                   = 0x4
	PROT_GROWSDOWN                              = 0x1000000
	PROT_GROWSUP                                = 0x2000000
	PROT_NONE                                   = 0x0
	PROT_READ                                   = 0x1
	PROT_WRITE                                  = 0x2
	PR_CAPBSET_DROP                             = 0x18
	PR_CAPBSET_READ                             = 0x17
	PR_CAP_AMBIENT                              = 0x2f
	PR_CAP_AMBIENT_CLEAR_ALL                    = 0x4
	PR_CAP_AMBIENT_IS_SET                       = 0x1
	PR_CAP_AMBIENT_LOWER                        = 0x3
	PR_CAP_AMBIENT_RAISE                        = 0x2
	PR_ENDIAN_BIG                               = 0x0
	PR_ENDIAN_LITTLE                            = 0x1
	PR_ENDIAN_PPC_LITTLE                        = 0x2
	PR_FPEMU_NOPRINT                            = 0x1
	PR_FPEMU_SIGFPE                             = 0x2
	PR_FP_EXC_ASYNC                             = 0x2
	PR_FP_EXC_DISABLED                          = 0x0
	PR_FP_EXC_DIV                               = 0x10000
	PR_FP_EXC_INV                               = 0x100000
	PR_FP_EXC_NONRECOV                          = 0x1
	PR_FP_EXC_OVF                               = 0x20000
	PR_FP_EXC_PRECISE                           = 0x3
	PR_FP_EXC_RES                               = 0x80000
	PR_FP_EXC_SW_ENABLE                         = 0x80
	PR_FP_EXC_UND                               = 0x40000
	PR_FP_MODE_FR                               = 0x1
	PR_FP_MODE_FRE                              = 0x2
	PR_FUTEX_HASH                               = 0x4e
	PR_FUTEX_HASH_GET_IMMUTABLE                 = 0x3
	PR_FUTEX_HASH_GET_SLOTS                     = 0x2
	PR_FUTEX_HASH_SET_SLOTS                     = 0x1
	PR_GET_AUXV                                 = 0x41555856
	PR_GET_CHILD_SUBREAPER                      = 0x25
	PR_GET_DUMPABLE                             = 0x3
	PR_GET_ENDIAN                               = 0x13
	PR_GET_FPEMU                                = 0x9
	PR_GET_FPEXC                                = 0xb
	PR_GET_FP_MODE                              = 0x2e
	PR_GET_IO_FLUSHER                           = 0x3a
	PR_GET_KEEPCAPS                             = 0x7
	PR_GET_MDWE                                 = 0x42
	PR_GET_MEMORY_MERGE                         = 0x44
	PR_GET_NAME                                 = 0x10
	PR_GET_NO_NEW_PRIVS                         = 0x27
	PR_GET_PDEATHSIG                            = 0x2
	PR_GET_SECCOMP                              = 0x15
	PR_GET_SECUREBITS                           = 0x1b
	PR_GET_SHADOW_STACK_STATUS                  = 0x4a
	PR_GET_SPECULATION_CTRL                     = 0x34
	PR_GET_TAGGED_ADDR_CTRL                     = 0x38
	PR_GET_THP_DISABLE                          = 0x2a
	PR_GET_TID_ADDRESS                          = 0x28
	PR_GET_TIMERSLACK                           = 0x1e
	PR_GET_TIMING                               = 0xd
	PR_GET_TSC                                  = 0x19
	PR_GET_UNALIGN                              = 0x5
	PR_LOCK_SHADOW_STACK_STATUS                 = 0x4c
	PR_MCE_KILL                                 = 0x21
	PR_MCE_KILL_CLEAR                           = 0x0
	PR_MCE_KILL_DEFAULT                         = 0x2
	PR_MCE_KILL_EARLY                           = 0x1
	PR_MCE_KILL_GET                             = 0x22
	PR_MCE_KILL_LATE                            = 0x0
	PR_MCE_KILL_SET                             = 0x1
	PR_MDWE_NO_INHERIT                          = 0x2
	PR_MDWE_REFUSE_EXEC_GAIN                    = 0x1
	PR_MPX_DISABLE_MANAGEMENT                   = 0x2c
	PR_MPX_ENABLE_MANAGEMENT                    = 0x2b
	PR_MTE_TAG_MASK                             = 0x7fff8
	PR_MTE_TAG_SHIFT                            = 0x3
	PR_MTE_TCF_ASYNC                            = 0x4
	PR_MTE_TCF_MASK                             = 0x6
	PR_MTE_TCF_NONE                             = 0x0
	PR_MTE_TCF_SHIFT                            = 0x1
	PR_MTE_TCF_SYNC                             = 0x2
	PR_PAC_APDAKEY                              = 0x4
	PR_PAC_APDBKEY                              = 0x8
	PR_PAC_APGAKEY                              = 0x10
	PR_PAC_APIAKEY                              = 0x1
	PR_PAC_APIBKEY                              = 0x2
	PR_PAC_GET_ENABLED_KEYS                     = 0x3d
	PR_PAC_RESET_KEYS                           = 0x36
	PR_PAC_SET_ENABLED_KEYS                     = 0x3c
	PR_PMLEN_MASK                               = 0x7f000000
	PR_PMLEN_SHIFT                              = 0x18
	PR_PPC_DEXCR_CTRL_CLEAR                     = 0x4
	PR_PPC_DEXCR_CTRL_CLEAR_ONEXEC              = 0x10
	PR_PPC_DEXCR_CTRL_EDITABLE                  = 0x1
	PR_PPC_DEXCR_CTRL_MASK                      = 0x1f
	PR_PPC_DEXCR_CTRL_SET                       = 0x2
	PR_PPC_DEXCR_CTRL_SET_ONEXEC                = 0x8
	PR_PPC_DEXCR_IBRTPD                         = 0x1
	PR_PPC_DEXCR_NPHIE                          = 0x3
	PR_PPC_DEXCR_SBHE                           = 0x0
	PR_PPC_DEXCR_SRAPD                          = 0x2
	PR_PPC_GET_DEXCR                            = 0x48
	PR_PPC_SET_DEXCR                            = 0x49
	PR_RISCV_CTX_SW_FENCEI_OFF                  = 0x1
	PR_RISCV_CTX_SW_FENCEI_ON                   = 0x0
	PR_RISCV_SCOPE_PER_PROCESS                  = 0x0
	PR_RISCV_SCOPE_PER_THREAD                   = 0x1
	PR_RISCV_SET_ICACHE_FLUSH_CTX               = 0x47
	PR_RISCV_V_GET_CONTROL                      = 0x46
	PR_RISCV_V_SET_CONTROL                      = 0x45
	PR_RISCV_V_VSTATE_CTRL_CUR_MASK             = 0x3
	PR_RISCV_V_VSTATE_CTRL_DEFAULT              = 0x0
	PR_RISCV_V_VSTATE_CTRL_INHERIT              = 0x10
	PR_RISCV_V_VSTATE_CTRL_MASK                 = 0x1f
	PR_RISCV_V_VSTATE_CTRL_NEXT_MASK            = 0xc
	PR_RISCV_V_VSTATE_CTRL_OFF                  = 0x1
	PR_RISCV_V_VSTATE_CTRL_ON                   = 0x2
	PR_SCHED_CORE                               = 0x3e
	PR_SCHED_CORE_CREATE                        = 0x1
	PR_SCHED_CORE_GET                           = 0x0
	PR_SCHED_CORE_MAX                           = 0x4
	PR_SCHED_CORE_SCOPE_PROCESS_GROUP           = 0x2
	PR_SCHED_CORE_SCOPE_THREAD                  = 0x0
	PR_SCHED_CORE_SCOPE_THREAD_GROUP            = 0x1
	PR_SCHED_CORE_SHARE_FROM                    = 0x3
	PR_SCHED_CORE_SHARE_TO                      = 0x2
	PR_SET_CHILD_SUBREAPER                      = 0x24
	PR_SET_DUMPABLE                             = 0x4
	PR_SET_ENDIAN                               = 0x14
	PR_SET_FPEMU                                = 0xa
	PR_SET_FPEXC                                = 0xc
	PR_SET_FP_MODE                              = 0x2d
	PR_SET_IO_FLUSHER                           = 0x39
	PR_SET_KEEPCAPS                             = 0x8
	PR_SET_MDWE                                 = 0x41
	PR_SET_MEMORY_MERGE                         = 0x43
	PR_SET_MM                                   = 0x23
	PR_SET_MM_ARG_END                           = 0x9
	PR_SET_MM_ARG_START                         = 0x8
	PR_SET_MM_AUXV                              = 0xc
	PR_SET_MM_BRK                               = 0x7
	PR_SET_MM_END_CODE                          = 0x2
	PR_SET_MM_END_DATA                          = 0x4
	PR_SET_MM_ENV_END                           = 0xb
	PR_SET_MM_ENV_START                         = 0xa
	PR_SET_MM_EXE_FILE                          = 0xd
	PR_SET_MM_MAP                               = 0xe
	PR_SET_MM_MAP_SIZE                          = 0xf
	PR_SET_MM_START_BRK                         = 0x6
	PR_SET_MM_START_CODE                        = 0x1
	PR_SET_MM_START_DATA                        = 0x3
	PR_SET_MM_START_STACK                       = 0x5
	PR_SET_NAME                                 = 0xf
	PR_SET_NO_NEW_PRIVS                         = 0x26
	PR_SET_PDEATHSIG                            = 0x1
	PR_SET_PTRACER                              = 0x59616d61
	PR_SET_SECCOMP                              = 0x16
	PR_SET_SECUREBITS                           = 0x1c
	PR_SET_SHADOW_STACK_STATUS                  = 0x4b
	PR_SET_SPECULATION_CTRL                     = 0x35
	PR_SET_SYSCALL_USER_DISPATCH                = 0x3b
	PR_SET_TAGGED_ADDR_CTRL                     = 0x37
	PR_SET_THP_DISABLE                          = 0x29
	PR_SET_TIMERSLACK                           = 0x1d
	PR_SET_TIMING                               = 0xe
	PR_SET_TSC                                  = 0x1a
	PR_SET_UNALIGN                              = 0x6
	PR_SET_VMA                                  = 0x53564d41
	PR_SET_VMA_ANON_NAME                        = 0x0
	PR_SHADOW_STACK_ENABLE                      = 0x1
	PR_SHADOW_STACK_PUSH                        = 0x4
	PR_SHADOW_STACK_WRITE                       = 0x2
	PR_SME_GET_VL                               = 0x40
	PR_SME_SET_VL                               = 0x3f
	PR_SME_SET_VL_ONEXEC                        = 0x40000
	PR_SME_VL_INHERIT                           = 0x20000
	PR_SME_VL_LEN_MASK                          = 0xffff
	PR_SPEC_DISABLE                             = 0x4
	PR_SPEC_DISABLE_NOEXEC                      = 0x10
	PR_SPEC_ENABLE                              = 0x2
	PR_SPEC_FORCE_DISABLE                       = 0x8
	PR_SPEC_INDIRECT_BRANCH                     = 0x1
	PR_SPEC_L1D_FLUSH                           = 0x2
	PR_SPEC_NOT_AFFECTED                        = 0x0
	PR_SPEC_PRCTL                               = 0x1
	PR_SPEC_STORE_BYPASS                        = 0x0
	PR_SVE_GET_VL                               = 0x33
	PR_SVE_SET_VL                               = 0x32
	PR_SVE_SET_VL_ONEXEC                        = 0x40000
	PR_SVE_VL_INHERIT                           = 0x20000
	PR_SVE_VL_LEN_MASK                          = 0xffff
	PR_SYS_DISPATCH_OFF                         = 0x0
	PR_SYS_DISPATCH_ON                          = 0x1
	PR_TAGGED_ADDR_ENABLE                       = 0x1
	PR_TASK_PERF_EVENTS_DISABLE                 = 0x1f
	PR_TASK_PERF_EVENTS_ENABLE                  = 0x20
	PR_TIMER_CREATE_RESTORE_IDS                 = 0x4d
	PR_TIMER_CREATE_RESTORE_IDS_GET             = 0x2
	PR_TIMER_CREATE_RESTORE_IDS_OFF             = 0x0
	PR_TIMER_CREATE_RESTORE_IDS_ON              = 0x1
	PR_TIMING_STATISTICAL                       = 0x0
	PR_TIMING_TIMESTAMP                         = 0x1
	PR_TSC_ENABLE                               = 0x1
	PR_TSC_SIGSEGV                              = 0x2
	PR_UNALIGN_NOPRINT                          = 0x1
	PR_UNALIGN_SIGBUS                           = 0x2
	PSTOREFS_MAGIC                              = 0x6165676c
	PTP_CLK_MAGIC                               = '='
	PTP_ENABLE_FEATURE                          = 0x1
	PTP_EXTTS_EDGES                             = 0x6
	PTP_EXTTS_EVENT_VALID                       = 0x1
	PTP_EXTTS_V1_VALID_FLAGS                    = 0x7
	PTP_EXTTS_VALID_FLAGS                       = 0x1f
	PTP_EXT_OFFSET                              = 0x10
	PTP_FALLING_EDGE                            = 0x4
	PTP_MAX_SAMPLES                             = 0x19
	PTP_PEROUT_DUTY_CYCLE                       = 0x2
	PTP_PEROUT_ONE_SHOT                         = 0x1
	PTP_PEROUT_PHASE                            = 0x4
	PTP_PEROUT_V1_VALID_FLAGS                   = 0x0
	PTP_PEROUT_VALID_FLAGS                      = 0x7
	PTP_PIN_GETFUNC                             = 0xc0603d06
	PTP_PIN_GETFUNC2                            = 0xc0603d0f
	PTP_RISING_EDGE                             = 0x2
	PTP_STRICT_FLAGS                            = 0x8
	PTP_SYS_OFFSET_EXTENDED                     = 0xc4c03d09
	PTP_SYS_OFFSET_EXTENDED2                    = 0xc4c03d12
	PTP_SYS_OFFSET_PRECISE                      = 0xc0403d08
	PTP_SYS_OFFSET_PRECISE2                     = 0xc0403d11
	PTRACE_ATTACH                               = 0x10
	PTRACE_CONT                                 = 0x7
	PTRACE_DETACH                               = 0x11
	PTRACE_EVENTMSG_SYSCALL_ENTRY               = 0x1
	PTRACE_EVENTMSG_SYSCALL_EXIT                = 0x2
	PTRACE_EVENT_CLONE                          = 0x3
	PTRACE_EVENT_EXEC                           = 0x4
	PTRACE_EVENT_EXIT                           = 0x6
	PTRACE_EVENT_FORK                           = 0x1
	PTRACE_EVENT_SECCOMP                        = 0x7
	PTRACE_EVENT_STOP                           = 0x80
	PTRACE_EVENT_VFORK                          = 0x2
	PTRACE_EVENT_VFORK_DONE                     = 0x5
	PTRACE_GETEVENTMSG                          = 0x4201
	PTRACE_GETREGS                              = 0xc
	PTRACE_GETREGSET                            = 0x4204
	PTRACE_GETSIGINFO                           = 0x4202
	PTRACE_GETSIGMASK                           = 0x420a
	PTRACE_GET_RSEQ_CONFIGURATION               = 0x420f
	PTRACE_GET_SYSCALL_INFO                     = 0x420e
	PTRACE_GET_SYSCALL_USER_DISPATCH_CONFIG     = 0x4211
	PTRACE_INTERRUPT                            = 0x4207
	PTRACE_KILL                                 = 0x8
	PTRACE_LISTEN                               = 0x4208
	PTRACE_O_EXITKILL                           = 0x100000
	PTRACE_O_MASK                               = 0x3000ff
	PTRACE_O_SUSPEND_SECCOMP                    = 0x200000
	PTRACE_O_TRACECLONE                         = 0x8
	PTRACE_O_TRACEEXEC                          = 0x10
	PTRACE_O_TRACEEXIT                          = 0x40
	PTRACE_O_TRACEFORK                          = 0x2
	PTRACE_O_TRACESECCOMP                       = 0x80
	PTRACE_O_TRACESYSGOOD                       = 0x1
	PTRACE_O_TRACEVFORK                         = 0x4
	PTRACE_O_TRACEVFORKDONE                     = 0x20
	PTRACE_PEEKDATA                             = 0x2
	PTRACE_PEEKSIGINFO                          = 0x4209
	PTRACE_PEEKSIGINFO_SHARED                   = 0x1
	PTRACE_PEEKTEXT                             = 0x1
	PTRACE_PEEKUSR                              = 0x3
	PTRACE_POKEDATA                             = 0x5
	PTRACE_POKETEXT                             = 0x4
	PTRACE_POKEUSR                              = 0x6
	PTRACE_SECCOMP_GET_FILTER                   = 0x420c
	PTRACE_SECCOMP_GET_METADATA                 = 0x420d
	PTRACE_SEIZE                                = 0x4206
	PTRACE_SETOPTIONS                           = 0x4200
	PTRACE_SETREGS                              = 0xd
	PTRACE_SETREGSET                            = 0x4205
	PTRACE_SETSIGINFO                           = 0x4203
	PTRACE_SETSIGMASK                           = 0x420b
	PTRACE_SET_SYSCALL_INFO                     = 0x4212
	PTRACE_SET_SYSCALL_USER_DISPATCH_CONFIG     = 0x4210
	PTRACE_SINGLESTEP                           = 0x9
	PTRACE_SYSCALL                              = 0x18
	PTRACE_SYSCALL_INFO_ENTRY                   = 0x1
	PTRACE_SYSCALL_INFO_EXIT                    = 0x2
	PTRACE_SYSCALL_INFO_NONE                    = 0x0
	PTRACE_SYSCALL_INFO_SECCOMP                 = 0x3
	PTRACE_TRACEME                              = 0x0
	P_ALL                                       = 0x0
	P_PGID                                      = 0x2
	P_PID                                       = 0x1
	P_PIDFD                                     = 0x3
	QNX4_SUPER_MAGIC                            = 0x2f
	QNX6_SUPER_MAGIC                            = 0x68191122
	RAMFS_MAGIC                                 = 0x858458f6
	RAW_PAYLOAD_DIGITAL                         = 0x3
	RAW_PAYLOAD_HCI                             = 0x2
	RAW_PAYLOAD_LLCP                            = 0x0
	RAW_PAYLOAD_NCI                             = 0x1
	RAW_PAYLOAD_PROPRIETARY                     = 0x4
	RDTGROUP_SUPER_MAGIC                        = 0x7655821
	REISERFS_SUPER_MAGIC                        = 0x52654973
	RENAME_EXCHANGE                             = 0x2
	RENAME_NOREPLACE                            = 0x1
	RENAME_WHITEOUT                             = 0x4
	RLIMIT_CORE                                 = 0x4
	RLIMIT_CPU                                  = 0x0
	RLIMIT_DATA                                 = 0x2
	RLIMIT_FSIZE                                = 0x1
	RLIMIT_LOCKS                                = 0xa
	RLIMIT_MSGQUEUE                             = 0xc
	RLIMIT_NICE                                 = 0xd
	RLIMIT_RTPRIO                               = 0xe
	RLIMIT_RTTIME                               = 0xf
	RLIMIT_SIGPENDING                           = 0xb
	RLIMIT_STACK                                = 0x3
	RLIM_INFINITY                               = 0xffffffffffffffff
	RTAX_ADVMSS                                 = 0x8
	RTAX_CC_ALGO                                = 0x10
	RTAX_CWND                                   = 0x7
	RTAX_FASTOPEN_NO_COOKIE                     = 0x11
	RTAX_FEATURES                               = 0xc
	RTAX_FEATURE_ALLFRAG                        = 0x8
	RTAX_FEATURE_ECN                            = 0x1
	RTAX_FEATURE_MASK                           = 0x1f
	RTAX_FEATURE_SACK                           = 0x2
	RTAX_FEATURE_TCP_USEC_TS                    = 0x10
	RTAX_FEATURE_TIMESTAMP                      = 0x4
	RTAX_HOPLIMIT                               = 0xa
	RTAX_INITCWND                               = 0xb
	RTAX_INITRWND                               = 0xe
	RTAX_LOCK                                   = 0x1
	RTAX_MAX                                    = 0x11
	RTAX_MTU                                    = 0x2
	RTAX_QUICKACK                               = 0xf
	RTAX_REORDERING                             = 0x9
	RTAX_RTO_MIN                                = 0xd
	RTAX_RTT                                    = 0x4
	RTAX_RTTVAR                                 = 0x5
	RTAX_SSTHRESH                               = 0x6
	RTAX_UNSPEC                                 = 0x0
	RTAX_WINDOW                                 = 0x3
	RTA_ALIGNTO                                 = 0x4
	RTA_MAX                                     = 0x1f
	RTCF_DIRECTSRC                              = 0x4000000
	RTCF_DOREDIRECT                             = 0x1000000
	RTCF_LOG                                    = 0x2000000
	RTCF_MASQ                                   = 0x400000
	RTCF_NAT                                    = 0x800000
	RTCF_VALVE                                  = 0x200000
	RTC_AF                                      = 0x20
	RTC_BSM_DIRECT                              = 0x1
	RTC_BSM_DISABLED                            = 0x0
	RTC_BSM_LEVEL                               = 0x2
	RTC_BSM_STANDBY                             = 0x3
	RTC_FEATURE_ALARM                           = 0x0
	RTC_FEATURE_ALARM_RES_2S                    = 0x3
	RTC_FEATURE_ALARM_RES_MINUTE                = 0x1
	RTC_FEATURE_ALARM_WAKEUP_ONLY               = 0x7
	RTC_FEATURE_BACKUP_SWITCH_MODE              = 0x6
	RTC_FEATURE_CNT                             = 0x8
	RTC_FEATURE_CORRECTION                      = 0x5
	RTC_FEATURE_NEED_WEEK_DAY                   = 0x2
	RTC_FEATURE_UPDATE_INTERRUPT                = 0x4
	RTC_IRQF                                    = 0x80
	RTC_MAX_FREQ                                = 0x2000
	RTC_PARAM_BACKUP_SWITCH_MODE                = 0x2
	RTC_PARAM_CORRECTION                        = 0x1
	RTC_PARAM_FEATURES                          = 0x0
	RTC_PF                                      = 0x40
	RTC_UF                                      = 0x10
	RTF_ADDRCLASSMASK                           = 0xf8000000
	RTF_ADDRCONF                                = 0x40000
	RTF_ALLONLINK                               = 0x20000
	RTF_BROADCAST                               = 0x10000000
	RTF_CACHE                                   = 0x1000000
	RTF_DEFAULT                                 = 0x10000
	RTF_DYNAMIC                                 = 0x10
	RTF_FLOW                                    = 0x2000000
	RTF_GATEWAY                                 = 0x2
	RTF_HOST                                    = 0x4
	RTF_INTERFACE                               = 0x40000000
	RTF_IRTT                                    = 0x100
	RTF_LINKRT                                  = 0x100000
	RTF_LOCAL                                   = 0x80000000
	RTF_MODIFIED                                = 0x20
	RTF_MSS                                     = 0x40
	RTF_MTU                                     = 0x40
	RTF_MULTICAST                               = 0x20000000
	RTF_NAT                                     = 0x8000000
	RTF_NOFORWARD                               = 0x1000
	RTF_NONEXTHOP                               = 0x200000
	RTF_NOPMTUDISC                              = 0x4000
	RTF_POLICY                                  = 0x4000000
	RTF_REINSTATE                               = 0x8
	RTF_REJECT                                  = 0x200
	RTF_STATIC                                  = 0x400
	RTF_THROW                                   = 0x2000
	RTF_UP                                      = 0x1
	RTF_WINDOW                                  = 0x80
	RTF_XRESOLVE                                = 0x800
	RTMGRP_DECnet_IFADDR                        = 0x1000
	RTMGRP_DECnet_ROUTE                         = 0x4000
	RTMGRP_IPV4_IFADDR                          = 0x10
	RTMGRP_IPV4_MROUTE                          = 0x20
	RTMGRP_IPV4_ROUTE                           = 0x40
	RTMGRP_IPV4_RULE                            = 0x80
	RTMGRP_IPV6_IFADDR                          = 0x100
	RTMGRP_IPV6_IFINFO                          = 0x800
	RTMGRP_IPV6_MROUTE                          = 0x200
	RTMGRP_IPV6_PREFIX                          = 0x20000
	RTMGRP_IPV6_ROUTE                           = 0x400
	RTMGRP_LINK                                 = 0x1
	RTMGRP_NEIGH                                = 0x4
	RTMGRP_NOTIFY                               = 0x2
	RTMGRP_TC                                   = 0x8
	RTM_BASE                                    = 0x10
	RTM_DELACTION                               = 0x31
	RTM_DELADDR                                 = 0x15
	RTM_DELADDRLABEL                            = 0x49
	RTM_DELANYCAST                              = 0x3d
	RTM_DELCHAIN                                = 0x65
	RTM_DELLINK                                 = 0x11
	RTM_DELLINKPROP                             = 0x6d
	RTM_DELMDB                                  = 0x55
	RTM_DELMULTICAST                            = 0x39
	RTM_DELNEIGH                                = 0x1d
	RTM_DELNETCONF                              = 0x51
	RTM_DELNEXTHOP                              = 0x69
	RTM_DELNEXTHOPBUCKET                        = 0x75
	RTM_DELNSID                                 = 0x59
	RTM_DELQDISC                                = 0x25
	RTM_DELROUTE                                = 0x19
	RTM_DELRULE                                 = 0x21
	RTM_DELTCLASS                               = 0x29
	RTM_DELTFILTER                              = 0x2d
	RTM_DELTUNNEL                               = 0x79
	RTM_DELVLAN                                 = 0x71
	RTM_F_CLONED                                = 0x200
	RTM_F_EQUALIZE                              = 0x400
	RTM_F_FIB_MATCH                             = 0x2000
	RTM_F_LOOKUP_TABLE                          = 0x1000
	RTM_F_NOTIFY                                = 0x100
	RTM_F_OFFLOAD                               = 0x4000
	RTM_F_OFFLOAD_FAILED                        = 0x20000000
	RTM_F_PREFIX                                = 0x800
	RTM_F_TRAP                                  = 0x8000
	RTM_GETACTION                               = 0x32
	RTM_GETADDR                                 = 0x16
	RTM_GETADDRLABEL                            = 0x4a
	RTM_GETANYCAST                              = 0x3e
	RTM_GETCHAIN                                = 0x66
	RTM_GETDCB                                  = 0x4e
	RTM_GETLINK                                 = 0x12
	RTM_GETLINKPROP                             = 0x6e
	RTM_GETMDB                                  = 0x56
	RTM_GETMULTICAST                            = 0x3a
	RTM_GETNEIGH                                = 0x1e
	RTM_GETNEIGHTBL                             = 0x42
	RTM_GETNETCONF                              = 0x52
	RTM_GETNEXTHOP                              = 0x6a
	RTM_GETNEXTHOPBUCKET                        = 0x76
	RTM_GETNSID                                 = 0x5a
	RTM_GETQDISC                                = 0x26
	RTM_GETROUTE                                = 0x1a
	RTM_GETRULE                                 = 0x22
	RTM_GETSTATS                                = 0x5e
	RTM_GETTCLASS                               = 0x2a
	RTM_GETTFILTER                              = 0x2e
	RTM_GETTUNNEL                               = 0x7a
	RTM_GETVLAN                                 = 0x72
	RTM_MAX                                     = 0x7b
	RTM_NEWACTION                               = 0x30
	RTM_NEWADDR                                 = 0x14
	RTM_NEWADDRLABEL                            = 0x48
	RTM_NEWANYCAST                              = 0x3c
	RTM_NEWCACHEREPORT                          = 0x60
	RTM_NEWCHAIN                                = 0x64
	RTM_NEWLINK                                 = 0x10
	RTM_NEWLINKPROP                             = 0x6c
	RTM_NEWMDB                                  = 0x54
	RTM_NEWMULTICAST                            = 0x38
	RTM_NEWNDUSEROPT                            = 0x44
	RTM_NEWNEIGH                                = 0x1c
	RTM_NEWNEIGHTBL                             = 0x40
	RTM_NEWNETCONF                              = 0x50
	RTM_NEWNEXTHOP                              = 0x68
	RTM_NEWNEXTHOPBUCKET                        = 0x74
	RTM_NEWNSID                                 = 0x58
	RTM_NEWPREFIX                               = 0x34
	RTM_NEWQDISC                                = 0x24
	RTM_NEWROUTE                                = 0x18
	RTM_NEWRULE                                 = 0x20
	RTM_NEWSTATS                                = 0x5c
	RTM_NEWTCLASS                               = 0x28
	RTM_NEWTFILTER                              = 0x2c
	RTM_NEWTUNNEL                               = 0x78
	RTM_NEWVLAN                                 = 0x70
	RTM_NR_FAMILIES                             = 0x1b
	RTM_NR_MSGTYPES                             = 0x6c
	RTM_SETDCB                                  = 0x4f
	RTM_SETLINK                                 = 0x13
	RTM_SETNEIGHTBL                             = 0x43
	RTM_SETSTATS                                = 0x5f
	RTNH_ALIGNTO                                = 0x4
	RTNH_COMPARE_MASK                           = 0x59
	RTNH_F_DEAD                                 = 0x1
	RTNH_F_LINKDOWN                             = 0x10
	RTNH_F_OFFLOAD                              = 0x8
	RTNH_F_ONLINK                               = 0x4
	RTNH_F_PERVASIVE                            = 0x2
	RTNH_F_TRAP                                 = 0x40
	RTNH_F_UNRESOLVED                           = 0x20
	RTN_MAX                                     = 0xb
	RTPROT_BABEL                                = 0x2a
	RTPROT_BGP                                  = 0xba
	RTPROT_BIRD                                 = 0xc
	RTPROT_BOOT                                 = 0x3
	RTPROT_DHCP                                 = 0x10
	RTPROT_DNROUTED                             = 0xd
	RTPROT_EIGRP                                = 0xc0
	RTPROT_GATED                                = 0x8
	RTPROT_ISIS                                 = 0xbb
	RTPROT_KEEPALIVED                           = 0x12
	RTPROT_KERNEL                               = 0x2
	RTPROT_MROUTED                              = 0x11
	RTPROT_MRT                                  = 0xa
	RTPROT_NTK                                  = 0xf
	RTPROT_OPENR                                = 0x63
	RTPROT_OSPF                                 = 0xbc
	RTPROT_OVN                                  = 0x54
	RTPROT_RA                                   = 0x9
	RTPROT_REDIRECT                             = 0x1
	RTPROT_RIP                                  = 0xbd
	RTPROT_STATIC                               = 0x4
	RTPROT_UNSPEC                               = 0x0
	RTPROT_XORP                                 = 0xe
	RTPROT_ZEBRA                                = 0xb
	RT_CLASS_DEFAULT                            = 0xfd
	RT_CLASS_LOCAL                              = 0xff
	RT_CLASS_MAIN                               = 0xfe
	RT_CLASS_MAX                                = 0xff
	RT_CLASS_UNSPEC                             = 0x0
	RUSAGE_CHILDREN                             = -0x1
	RUSAGE_SELF                                 = 0x0
	RUSAGE_THREAD                               = 0x1
	RWF_APPEND                                  = 0x10
	RWF_ATOMIC                                  = 0x40
	RWF_DONTCACHE                               = 0x80
	RWF_DSYNC                                   = 0x2
	RWF_HIPRI                                   = 0x1
	RWF_NOAPPEND                                = 0x20
	RWF_NOWAIT                                  = 0x8
	RWF_SUPPORTED                               = 0xff
	RWF_SYNC                                    = 0x4
	RWF_WRITE_LIFE_NOT_SET                      = 0x0
	SCHED_BATCH                                 = 0x3
	SCHED_DEADLINE                              = 0x6
	SCHED_EXT                                   = 0x7
	SCHED_FIFO                                  = 0x1
	SCHED_FLAG_ALL                              = 0x7f
	SCHED_FLAG_DL_OVERRUN                       = 0x4
	SCHED_FLAG_KEEP_ALL                         = 0x18
	SCHED_FLAG_KEEP_PARAMS                      = 0x10
	SCHED_FLAG_KEEP_POLICY                      = 0x8
	SCHED_FLAG_RECLAIM                          = 0x2
	SCHED_FLAG_RESET_ON_FORK                    = 0x1
	SCHED_FLAG_UTIL_CLAMP                       = 0x60
	SCHED_FLAG_UTIL_CLAMP_MAX                   = 0x40
	SCHED_FLAG_UTIL_CLAMP_MIN                   = 0x20
	SCHED_IDLE                                  = 0x5
	SCHED_NORMAL                                = 0x0
	SCHED_RESET_ON_FORK                         = 0x40000000
	SCHED_RR                                    = 0x2
	SCM_CREDENTIALS                             = 0x2
	SCM_PIDFD                                   = 0x4
	SCM_RIGHTS                                  = 0x1
	SCM_SECURITY                                = 0x3
	SCM_TIMESTAMP                               = 0x1d
	SC_LOG_FLUSH                                = 0x100000
	SECCOMP_ADDFD_FLAG_SEND                     = 0x2
	SECCOMP_ADDFD_FLAG_SETFD                    = 0x1
	SECCOMP_FILTER_FLAG_LOG                     = 0x2
	SECCOMP_FILTER_FLAG_NEW_LISTENER            = 0x8
	SECCOMP_FILTER_FLAG_SPEC_ALLOW              = 0x4
	SECCOMP_FILTER_FLAG_TSYNC                   = 0x1
	SECCOMP_FILTER_FLAG_TSYNC_ESRCH             = 0x10
	SECCOMP_FILTER_FLAG_WAIT_KILLABLE_RECV      = 0x20
	SECCOMP_GET_ACTION_AVAIL                    = 0x2
	SECCOMP_GET_NOTIF_SIZES                     = 0x3
	SECCOMP_IOCTL_NOTIF_RECV                    = 0xc0502100
	SECCOMP_IOCTL_NOTIF_SEND                    = 0xc0182101
	SECCOMP_IOC_MAGIC                           = '!'
	SECCOMP_MODE_DISABLED                       = 0x0
	SECCOMP_MODE_FILTER                         = 0x2
	SECCOMP_MODE_STRICT                         = 0x1
	SECCOMP_RET_ACTION                          = 0x7fff0000
	SECCOMP_RET_ACTION_FULL                     = 0xffff0000
	SECCOMP_RET_ALLOW                           = 0x7fff0000
	SECCOMP_RET_DATA                            = 0xffff
	SECCOMP_RET_ERRNO                           = 0x50000
	SECCOMP_RET_KILL                            = 0x0
	SECCOMP_RET_KILL_PROCESS                    = 0x80000000
	SECCOMP_RET_KILL_THREAD                     = 0x0
	SECCOMP_RET_LOG                             = 0x7ffc0000
	SECCOMP_RET_TRACE                           = 0x7ff00000
	SECCOMP_RET_TRAP                            = 0x30000
	SECCOMP_RET_USER_NOTIF                      = 0x7fc00000
	SECCOMP_SET_MODE_FILTER                     = 0x1
	SECCOMP_SET_MODE_STRICT                     = 0x0
	SECCOMP_USER_NOTIF_FD_SYNC_WAKE_UP          = 0x1
	SECCOMP_USER_NOTIF_FLAG_CONTINUE            = 0x1
	SECRETMEM_MAGIC                             = 0x5345434d
	SECURITYFS_MAGIC                            = 0x73636673
	SEEK_CUR                                    = 0x1
	SEEK_DATA                                   = 0x3
	SEEK_END                                    = 0x2
	SEEK_HOLE                                   = 0x4
	SEEK_MAX                                    = 0x4
	SEEK_SET                                    = 0x0
	SELINUX_MAGIC                               = 0xf97cff8c
	SHUT_RD                                     = 0x0
	SHUT_RDWR                                   = 0x2
	SHUT_WR                                     = 0x1
	SIOCADDDLCI                                 = 0x8980
	SIOCADDMULTI                                = 0x8931
	SIOCADDRT                                   = 0x890b
	SIOCBONDCHANGEACTIVE                        = 0x8995
	SIOCBONDENSLAVE                             = 0x8990
	SIOCBONDINFOQUERY                           = 0x8994
	SIOCBONDRELEASE                             = 0x8991
	SIOCBONDSETHWADDR                           = 0x8992
	SIOCBONDSLAVEINFOQUERY                      = 0x8993
	SIOCBRADDBR                                 = 0x89a0
	SIOCBRADDIF                                 = 0x89a2
	SIOCBRDELBR                                 = 0x89a1
	SIOCBRDELIF                                 = 0x89a3
	SIOCDARP                                    = 0x8953
	SIOCDELDLCI                                 = 0x8981
	SIOCDELMULTI                                = 0x8932
	SIOCDELRT                                   = 0x890c
	SIOCDEVPRIVATE                              = 0x89f0
	SIOCDIFADDR                                 = 0x8936
	SIOCDRARP                                   = 0x8960
	SIOCETHTOOL                                 = 0x8946
	SIOCGARP                                    = 0x8954
	SIOCGETLINKNAME                             = 0x89e0
	SIOCGETNODEID                               = 0x89e1
	SIOCGHWTSTAMP                               = 0x89b1
	SIOCGIFADDR                                 = 0x8915
	SIOCGIFBR                                   = 0x8940
	SIOCGIFBRDADDR                              = 0x8919
	SIOCGIFCONF                                 = 0x8912
	SIOCGIFCOUNT                                = 0x8938
	SIOCGIFDSTADDR                              = 0x8917
	SIOCGIFENCAP                                = 0x8925
	SIOCGIFFLAGS                                = 0x8913
	SIOCGIFHWADDR                               = 0x8927
	SIOCGIFINDEX                                = 0x8933
	SIOCGIFMAP                                  = 0x8970
	SIOCGIFMEM                                  = 0x891f
	SIOCGIFMETRIC                               = 0x891d
	SIOCGIFMTU                                  = 0x8921
	SIOCGIFNAME                                 = 0x8910
	SIOCGIFNETMASK                              = 0x891b
	SIOCGIFPFLAGS                               = 0x8935
	SIOCGIFSLAVE                                = 0x8929
	SIOCGIFTXQLEN                               = 0x8942
	SIOCGIFVLAN                                 = 0x8982
	SIOCGMIIPHY                                 = 0x8947
	SIOCGMIIREG                                 = 0x8948
	SIOCGPPPCSTATS                              = 0x89f2
	SIOCGPPPSTATS                               = 0x89f0
	SIOCGPPPVER                                 = 0x89f1
	SIOCGRARP                                   = 0x8961
	SIOCGSKNS                                   = 0x894c
	SIOCGSTAMP                                  = 0x8906
	SIOCGSTAMPNS                                = 0x8907
	SIOCGSTAMPNS_OLD                            = 0x8907
	SIOCGSTAMP_OLD                              = 0x8906
	SIOCKCMATTACH                               = 0x89e0
	SIOCKCMCLONE                                = 0x89e2
	SIOCKCMUNATTACH                             = 0x89e1
	SIOCOUTQNSD                                 = 0x894b
	SIOCPROTOPRIVATE                            = 0x89e0
	SIOCRTMSG                                   = 0x890d
	SIOCSARP                                    = 0x8955
	SIOCSHWTSTAMP                               = 0x89b0
	SIOCSIFADDR                                 = 0x8916
	SIOCSIFBR                                   = 0x8941
	SIOCSIFBRDADDR                              = 0x891a
	SIOCSIFDSTADDR                              = 0x8918
	SIOCSIFENCAP                                = 0x8926
	SIOCSIFFLAGS                                = 0x8914
	SIOCSIFHWADDR                               = 0x8924
	SIOCSIFHWBROADCAST                          = 0x8937
	SIOCSIFLINK                                 = 0x8911
	SIOCSIFMAP                                  = 0x8971
	SIOCSIFMEM                                  = 0x8920
	SIOCSIFMETRIC                               = 0x891e
	SIOCSIFMTU                                  = 0x8922
	SIOCSIFNAME                                 = 0x8923
	SIOCSIFNETMASK                              = 0x891c
	SIOCSIFPFLAGS                               = 0x8934
	SIOCSIFSLAVE                                = 0x8930
	SIOCSIFTXQLEN                               = 0x8943
	SIOCSIFVLAN                                 = 0x8983
	SIOCSMIIREG                                 = 0x8949
	SIOCSRARP                                   = 0x8962
	SIOCWANDEV                                  = 0x894a
	SK_DIAG_BPF_STORAGE_MAX                     = 0x3
	SK_DIAG_BPF_STORAGE_REQ_MAX                 = 0x1
	SMACK_MAGIC                                 = 0x43415d53
	SMART_AUTOSAVE                              = 0xd2
	SMART_AUTO_OFFLINE                          = 0xdb
	SMART_DISABLE                               = 0xd9
	SMART_ENABLE                                = 0xd8
	SMART_HCYL_PASS                             = 0xc2
	SMART_IMMEDIATE_OFFLINE                     = 0xd4
	SMART_LCYL_PASS                             = 0x4f
	SMART_READ_LOG_SECTOR                       = 0xd5
	SMART_READ_THRESHOLDS                       = 0xd1
	SMART_READ_VALUES                           = 0xd0
	SMART_SAVE                                  = 0xd3
	SMART_STATUS                                = 0xda
	SMART_WRITE_LOG_SECTOR                      = 0xd6
	SMART_WRITE_THRESHOLDS                      = 0xd7
	SMB2_SUPER_MAGIC                            = 0xfe534d42
	SMB_SUPER_MAGIC                             = 0x517b
	SOCKFS_MAGIC                                = 0x534f434b
	SOCK_BUF_LOCK_MASK                          = 0x3
	SOCK_DCCP                                   = 0x6
	SOCK_DESTROY                                = 0x15
	SOCK_DIAG_BY_FAMILY                         = 0x14
	SOCK_IOC_TYPE                               = 0x89
	SOCK_PACKET                                 = 0xa
	SOCK_RAW                                    = 0x3
	SOCK_RCVBUF_LOCK                            = 0x2
	SOCK_RDM                                    = 0x4
	SOCK_SEQPACKET                              = 0x5
	SOCK_SNDBUF_LOCK                            = 0x1
	SOCK_TXREHASH_DEFAULT                       = 0xff
	SOCK_TXREHASH_DISABLED                      = 0x0
	SOCK_TXREHASH_ENABLED                       = 0x1
	SOL_AAL                                     = 0x109
	SOL_ALG                                     = 0x117
	SOL_ATM                                     = 0x108
	SOL_CAIF                                    = 0x116
	SOL_CAN_BASE                                = 0x64
	SOL_CAN_RAW                                 = 0x65
	SOL_DCCP                                    = 0x10d
	SOL_DECNET                                  = 0x105
	SOL_ICMPV6                                  = 0x3a
	SOL_IP                                      = 0x0
	SOL_IPV6                                    = 0x29
	SOL_IRDA                                    = 0x10a
	SOL_IUCV                                    = 0x115
	SOL_KCM                                     = 0x119
	SOL_LLC                                     = 0x10c
	SOL_MCTP                                    = 0x11d
	SOL_MPTCP                                   = 0x11c
	SOL_NETBEUI                                 = 0x10b
	SOL_NETLINK                                 = 0x10e
	SOL_NFC                                     = 0x118
	SOL_PACKET                                  = 0x107
	SOL_PNPIPE                                  = 0x113
	SOL_PPPOL2TP                                = 0x111
	SOL_RAW                                     = 0xff
	SOL_RDS                                     = 0x114
	SOL_RXRPC                                   = 0x110
	SOL_SMC                                     = 0x11e
	SOL_TCP                                     = 0x6
	SOL_TIPC                                    = 0x10f
	SOL_TLS                                     = 0x11a
	SOL_UDP                                     = 0x11
	SOL_VSOCK                                   = 0x11f
	SOL_X25                                     = 0x106
	SOL_XDP                                     = 0x11b
	SOMAXCONN                                   = 0x1000
	SO_ATTACH_FILTER                            = 0x1a
	SO_DEBUG                                    = 0x1
	SO_DETACH_BPF                               = 0x1b
	SO_DETACH_FILTER                            = 0x1b
	SO_EE_CODE_TXTIME_INVALID_PARAM             = 0x1
	SO_EE_CODE_TXTIME_MISSED                    = 0x2
	SO_EE_CODE_ZEROCOPY_COPIED                  = 0x1
	SO_EE_ORIGIN_ICMP                           = 0x2
	SO_EE_ORIGIN_ICMP6                          = 0x3
	SO_EE_ORIGIN_LOCAL                          = 0x1
	SO_EE_ORIGIN_NONE                           = 0x0
	SO_EE_ORIGIN_TIMESTAMPING                   = 0x4
	SO_EE_ORIGIN_TXSTATUS                       = 0x4
	SO_EE_ORIGIN_TXTIME                         = 0x6
	SO_EE_ORIGIN_ZEROCOPY                       = 0x5
	SO_EE_RFC4884_FLAG_INVALID                  = 0x1
	SO_GET_FILTER                               = 0x1a
	SO_NO_CHECK                                 = 0xb
	SO_PEERNAME                                 = 0x1c
	SO_PRIORITY                                 = 0xc
	SO_TIMESTAMP                                = 0x1d
	SO_TIMESTAMP_OLD                            = 0x1d
	SO_VM_SOCKETS_BUFFER_MAX_SIZE               = 0x2
	SO_VM_SOCKETS_BUFFER_MIN_SIZE               = 0x1
	SO_VM_SOCKETS_BUFFER_SIZE                   = 0x0
	SO_VM_SOCKETS_CONNECT_TIMEOUT               = 0x6
	SO_VM_SOCKETS_CONNECT_TIMEOUT_NEW           = 0x8
	SO_VM_SOCKETS_CONNECT_TIMEOUT_OLD           = 0x6
	SO_VM_SOCKETS_NONBLOCK_TXRX                 = 0x7
	SO_VM_SOCKETS_PEER_HOST_VM_ID               = 0x3
	SO_VM_SOCKETS_TRUSTED                       = 0x5
	SPLICE_F_GIFT                               = 0x8
	SPLICE_F_MORE                               = 0x4
	SPLICE_F_MOVE                               = 0x1
	SPLICE_F_NONBLOCK                           = 0x2
	SQUASHFS_MAGIC                              = 0x73717368
	STACK_END_MAGIC                             = 0x57ac6e9d
	STATX_ALL                                   = 0xfff
	STATX_ATIME                                 = 0x20
	STATX_ATTR_APPEND                           = 0x20
	STATX_ATTR_AUTOMOUNT                        = 0x1000
	STATX_ATTR_COMPRESSED                       = 0x4
	STATX_ATTR_DAX                              = 0x200000
	STATX_ATTR_ENCRYPTED                        = 0x800
	STATX_ATTR_IMMUTABLE                        = 0x10
	STATX_ATTR_MOUNT_ROOT                       = 0x2000
	STATX_ATTR_NODUMP                           = 0x40
	STATX_ATTR_VERITY                           = 0x100000
	STATX_ATTR_WRITE_ATOMIC                     = 0x400000
	STATX_BASIC_STATS                           = 0x7ff
	STATX_BLOCKS                                = 0x400
	STATX_BTIME                                 = 0x800
	STATX_CTIME                                 = 0x80
	STATX_DIOALIGN                              = 0x2000
	STATX_DIO_READ_ALIGN                        = 0x20000
	STATX_GID                                   = 0x10
	STATX_INO                                   = 0x100
	STATX_MNT_ID                                = 0x1000
	STATX_MNT_ID_UNIQUE                         = 0x4000
	STATX_MODE                                  = 0x2
	STATX_MTIME                                 = 0x40
	STATX_NLINK                                 = 0x4
	STATX_SIZE                                  = 0x200
	STATX_SUBVOL                                = 0x8000
	STATX_TYPE                                  = 0x1
	STATX_UID                                   = 0x8
	STATX_WRITE_ATOMIC                          = 0x10000
	STATX__RESERVED                             = 0x80000000
	SYNC_FILE_RANGE_WAIT_AFTER                  = 0x4
	SYNC_FILE_RANGE_WAIT_BEFORE                 = 0x1
	SYNC_FILE_RANGE_WRITE                       = 0x2
	SYNC_FILE_RANGE_WRITE_AND_WAIT              = 0x7
	SYSFS_MAGIC                                 = 0x62656572
	S_BLKSIZE                                   = 0x200
	S_IEXEC                                     = 0x40
	S_IFBLK                                     = 0x6000
	S_IFCHR                                     = 0x2000
	S_IFDIR                                     = 0x4000
	S_IFIFO                                     = 0x1000
	S_IFLNK                                     = 0xa000
	S_IFMT                                      = 0xf000
	S_IFREG                                     = 0x8000
	S_IFSOCK                                    = 0xc000
	S_IREAD                                     = 0x100
	S_IRGRP                                     = 0x20
	S_IROTH                                     = 0x4
	S_IRUSR                                     = 0x100
	S_IRWXG                                     = 0x38
	S_IRWXO                                     = 0x7
	S_IRWXU                                     = 0x1c0
	S_ISGID                                     = 0x400
	S_ISUID                                     = 0x800
	S_ISVTX                                     = 0x200
	S_IWGRP                                     = 0x10
	S_IWOTH                                     = 0x2
	S_IWRITE                                    = 0x80
	S_IWUSR                                     = 0x80
	S_IXGRP                                     = 0x8
	S_IXOTH                                     = 0x1
	S_IXUSR                                     = 0x40
	TAB0                                        = 0x0
	TASKSTATS_CMD_ATTR_MAX                      = 0x4
	TASKSTATS_CMD_MAX                           = 0x2
	TASKSTATS_GENL_NAME                         = "TASKSTATS"
	TASKSTATS_GENL_VERSION                      = 0x1
	TASKSTATS_TYPE_MAX                          = 0x6
	TASKSTATS_VERSION                           = 0x10
	TCIFLUSH                                    = 0x0
	TCIOFF                                      = 0x2
	TCIOFLUSH                                   = 0x2
	TCION                                       = 0x3
	TCOFLUSH                                    = 0x1
	TCOOFF                                      = 0x0
	TCOON                                       = 0x1
	TCPOPT_EOL                                  = 0x0
	TCPOPT_MAXSEG                               = 0x2
	TCPOPT_NOP                                  = 0x1
	TCPOPT_SACK                                 = 0x5
	TCPOPT_SACK_PERMITTED                       = 0x4
	TCPOPT_TIMESTAMP                            = 0x8
	TCPOPT_TSTAMP_HDR                           = 0x101080a
	TCPOPT_WINDOW                               = 0x3
	TCP_CC_INFO                                 = 0x1a
	TCP_CM_INQ                                  = 0x24
	TCP_CONGESTION                              = 0xd
	TCP_COOKIE_IN_ALWAYS                        = 0x1
	TCP_COOKIE_MAX                              = 0x10
	TCP_COOKIE_MIN                              = 0x8
	TCP_COOKIE_OUT_NEVER                        = 0x2
	TCP_COOKIE_PAIR_SIZE                        = 0x20
	TCP_COOKIE_TRANSACTIONS                     = 0xf
	TCP_CORK                                    = 0x3
	TCP_DEFER_ACCEPT                            = 0x9
	TCP_FASTOPEN                                = 0x17
	TCP_FASTOPEN_CONNECT                        = 0x1e
	TCP_FASTOPEN_KEY                            = 0x21
	TCP_FASTOPEN_NO_COOKIE                      = 0x22
	TCP_INFO                                    = 0xb
	TCP_INQ                                     = 0x24
	TCP_KEEPCNT                                 = 0x6
	TCP_KEEPIDLE                                = 0x4
	TCP_KEEPINTVL                               = 0x5
	TCP_LINGER2                                 = 0x8
	TCP_MAXSEG                                  = 0x2
	TCP_MAXWIN                                  = 0xffff
	TCP_MAX_WINSHIFT                            = 0xe
	TCP_MD5SIG                                  = 0xe
	TCP_MD5SIG_EXT                              = 0x20
	TCP_MD5SIG_FLAG_IFINDEX                     = 0x2
	TCP_MD5SIG_FLAG_PREFIX                      = 0x1
	TCP_MD5SIG_MAXKEYLEN                        = 0x50
	TCP_MSS                                     = 0x200
	TCP_MSS_DEFAULT                             = 0x218
	TCP_MSS_DESIRED                             = 0x4c4
	TCP_NODELAY                                 = 0x1
	TCP_NOTSENT_LOWAT                           = 0x19
	TCP_QUEUE_SEQ                               = 0x15
	TCP_QUICKACK                                = 0xc
	TCP_REPAIR                                  = 0x13
	TCP_REPAIR_OFF                              = 0x0
	TCP_REPAIR_OFF_NO_WP                        = -0x1
	TCP_REPAIR_ON                               = 0x1
	TCP_REPAIR_OPTIONS                          = 0x16
	TCP_REPAIR_QUEUE                            = 0x14
	TCP_REPAIR_WINDOW                           = 0x1d
	TCP_SAVED_SYN                               = 0x1c
	TCP_SAVE_SYN                                = 0x1b
	TCP_SYNCNT                                  = 0x7
	TCP_S_DATA_IN                               = 0x4
	TCP_S_DATA_OUT                              = 0x8
	TCP_THIN_DUPACK                             = 0x11
	TCP_THIN_LINEAR_TIMEOUTS                    = 0x10
	TCP_TIMESTAMP                               = 0x18
	TCP_TX_DELAY                                = 0x25
	TCP_ULP                                     = 0x1f
	TCP_USER_TIMEOUT                            = 0x12
	TCP_WINDOW_CLAMP                            = 0xa
	TCP_ZEROCOPY_RECEIVE                        = 0x23
	TFD_TIMER_ABSTIME                           = 0x1
	TFD_TIMER_CANCEL_ON_SET                     = 0x2
	TIMER_ABSTIME                               = 0x1
	TIOCM_DTR                                   = 0x2
	TIOCM_LE                                    = 0x1
	TIOCM_RTS                                   = 0x4
	TIOCPKT_DATA                                = 0x0
	TIOCPKT_DOSTOP                              = 0x20
	TIOCPKT_FLUSHREAD                           = 0x1
	TIOCPKT_FLUSHWRITE                          = 0x2
	TIOCPKT_IOCTL                               = 0x40
	TIOCPKT_NOSTOP                              = 0x10
	TIOCPKT_START                               = 0x8
	TIOCPKT_STOP                                = 0x4
	TIPC_ADDR_ID                                = 0x3
	TIPC_ADDR_MCAST                             = 0x1
	TIPC_ADDR_NAME                              = 0x2
	TIPC_ADDR_NAMESEQ                           = 0x1
	TIPC_AEAD_ALG_NAME                          = 0x20
	TIPC_AEAD_KEYLEN_MAX                        = 0x24
	TIPC_AEAD_KEYLEN_MIN                        = 0x14
	TIPC_AEAD_KEY_SIZE_MAX                      = 0x48
	TIPC_CFG_SRV                                = 0x0
	TIPC_CLUSTER_BITS                           = 0xc
	TIPC_CLUSTER_MASK                           = 0xfff000
	TIPC_CLUSTER_OFFSET                         = 0xc
	TIPC_CLUSTER_SIZE                           = 0xfff
	TIPC_CONN_SHUTDOWN                          = 0x5
	TIPC_CONN_TIMEOUT                           = 0x82
	TIPC_CRITICAL_IMPORTANCE                    = 0x3
	TIPC_DESTNAME                               = 0x3
	TIPC_DEST_DROPPABLE                         = 0x81
	TIPC_ERRINFO                                = 0x1
	TIPC_ERR_NO_NAME                            = 0x1
	TIPC_ERR_NO_NODE                            = 0x3
	TIPC_ERR_NO_PORT                            = 0x2
	TIPC_ERR_OVERLOAD                           = 0x4
	TIPC_GROUP_JOIN                             = 0x87
	TIPC_GROUP_LEAVE                            = 0x88
	TIPC_GROUP_LOOPBACK                         = 0x1
	TIPC_GROUP_MEMBER_EVTS                      = 0x2
	TIPC_HIGH_IMPORTANCE                        = 0x2
	TIPC_IMPORTANCE                             = 0x7f
	TIPC_LINK_STATE                             = 0x2
	TIPC_LOW_IMPORTANCE                         = 0x0
	TIPC_MAX_BEARER_NAME                        = 0x20
	TIPC_MAX_IF_NAME                            = 0x10
	TIPC_MAX_LINK_NAME                          = 0x44
	TIPC_MAX_MEDIA_NAME                         = 0x10
	TIPC_MAX_USER_MSG_SIZE                      = 0x101d0
	TIPC_MCAST_BROADCAST                        = 0x85
	TIPC_MCAST_REPLICAST                        = 0x86
	TIPC_MEDIUM_IMPORTANCE                      = 0x1
	TIPC_NODEID_LEN                             = 0x10
	TIPC_NODELAY                                = 0x8a
	TIPC_NODE_BITS                              = 0xc
	TIPC_NODE_MASK                              = 0xfff
	TIPC_NODE_OFFSET                            = 0x0
	TIPC_NODE_RECVQ_DEPTH                       = 0x83
	TIPC_NODE_SIZE                              = 0xfff
	TIPC_NODE_STATE                             = 0x0
	TIPC_OK                                     = 0x0
	TIPC_PUBLISHED                              = 0x1
	TIPC_REKEYING_NOW                           = 0xffffffff
	TIPC_RESERVED_TYPES                         = 0x40
	TIPC_RETDATA                                = 0x2
	TIPC_SERVICE_ADDR                           = 0x2
	TIPC_SERVICE_RANGE                          = 0x1
	TIPC_SOCKET_ADDR                            = 0x3
	TIPC_SOCK_RECVQ_DEPTH                       = 0x84
	TIPC_SOCK_RECVQ_USED                        = 0x89
	TIPC_SRC_DROPPABLE                          = 0x80
	TIPC_SUBSCR_TIMEOUT                         = 0x3
	TIPC_SUB_CANCEL                             = 0x4
	TIPC_SUB_PORTS                              = 0x1
	TIPC_SUB_SERVICE                            = 0x2
	TIPC_TOP_SRV                                = 0x1
	TIPC_WAIT_FOREVER                           = 0xffffffff
	TIPC_WITHDRAWN                              = 0x2
	TIPC_ZONE_BITS                              = 0x8
	TIPC_ZONE_CLUSTER_MASK                      = 0xfffff000
	TIPC_ZONE_MASK                              = 0xff000000
	TIPC_ZONE_OFFSET                            = 0x18
	TIPC_ZONE_SCOPE                             = 0x1
	TIPC_ZONE_SIZE                              = 0xff
	TMPFS_MAGIC                                 = 0x1021994
	TPACKET_ALIGNMENT                           = 0x10
	TPACKET_HDRLEN                              = 0x34
	TP_STATUS_AVAILABLE                         = 0x0
	TP_STATUS_BLK_TMO                           = 0x20
	TP_STATUS_COPY                              = 0x2
	TP_STATUS_CSUMNOTREADY                      = 0x8
	TP_STATUS_CSUM_VALID                        = 0x80
	TP_STATUS_GSO_TCP                           = 0x100
	TP_STATUS_KERNEL                            = 0x0
	TP_STATUS_LOSING                            = 0x4
	TP_STATUS_SENDING                           = 0x2
	TP_STATUS_SEND_REQUEST                      = 0x1
	TP_STATUS_TS_RAW_HARDWARE                   = 0x80000000
	TP_STATUS_TS_SOFTWARE                       = 0x20000000
	TP_STATUS_TS_SYS_HARDWARE                   = 0x40000000
	TP_STATUS_USER                              = 0x1
	TP_STATUS_VLAN_TPID_VALID                   = 0x40
	TP_STATUS_VLAN_VALID                        = 0x10
	TP_STATUS_WRONG_FORMAT                      = 0x4
	TRACEFS_MAGIC                               = 0x74726163
	TS_COMM_LEN                                 = 0x20
	UBI_IOCECNFO                                = 0xc01c6f06
	UDF_SUPER_MAGIC                             = 0x15013346
	UDP_CORK                                    = 0x1
	UDP_ENCAP                                   = 0x64
	UDP_ENCAP_ESPINUDP                          = 0x2
	UDP_ENCAP_ESPINUDP_NON_IKE                  = 0x1
	UDP_ENCAP_GTP0                              = 0x4
	UDP_ENCAP_GTP1U                             = 0x5
	UDP_ENCAP_L2TPINUDP                         = 0x3
	UDP_GRO                                     = 0x68
	UDP_NO_CHECK6_RX                            = 0x66
	UDP_NO_CHECK6_TX                            = 0x65
	UDP_SEGMENT                                 = 0x67
	UMOUNT_NOFOLLOW                             = 0x8
	USBDEVICE_SUPER_MAGIC                       = 0x9fa2
	UTIME_NOW                                   = 0x3fffffff
	UTIME_OMIT                                  = 0x3ffffffe
	V9FS_MAGIC                                  = 0x1021997
	VERASE                                      = 0x2
	VINTR                                       = 0x0
	VKILL                                       = 0x3
	VLNEXT                                      = 0xf
	VMADDR_CID_ANY                              = 0xffffffff
	VMADDR_CID_HOST                             = 0x2
	VMADDR_CID_HYPERVISOR                       = 0x0
	VMADDR_CID_LOCAL                            = 0x1
	VMADDR_FLAG_TO_HOST                         = 0x1
	VMADDR_PORT_ANY                             = 0xffffffff
	VM_SOCKETS_INVALID_VERSION                  = 0xffffffff
	VQUIT                                       = 0x1
	VT0                                         = 0x0
	WAKE_MAGIC                                  = 0x20
	WALL                                        = 0x40000000
	WCLONE                                      = 0x80000000
	WCONTINUED                                  = 0x8
	WDIOC_SETPRETIMEOUT                         = 0xc0045708
	WDIOC_SETTIMEOUT                            = 0xc0045706
	WDIOF_ALARMONLY                             = 0x400
	WDIOF_CARDRESET                             = 0x20
	WDIOF_EXTERN1                               = 0x4
	WDIOF_EXTERN2                               = 0x8
	WDIOF_FANFAULT                              = 0x2
	WDIOF_KEEPALIVEPING                         = 0x8000
	WDIOF_MAGICCLOSE                            = 0x100
	WDIOF_OVERHEAT                              = 0x1
	WDIOF_POWEROVER                             = 0x40
	WDIOF_POWERUNDER                            = 0x10
	WDIOF_PRETIMEOUT                            = 0x200
	WDIOF_SETTIMEOUT                            = 0x80
	WDIOF_UNKNOWN                               = -0x1
	WDIOS_DISABLECARD                           = 0x1
	WDIOS_ENABLECARD                            = 0x2
	WDIOS_TEMPPANIC                             = 0x4
	WDIOS_UNKNOWN                               = -0x1
	WEXITED                                     = 0x4
	WGALLOWEDIP_A_MAX                           = 0x4
	WGDEVICE_A_MAX                              = 0x8
	WGPEER_A_MAX                                = 0xa
	WG_CMD_MAX                                  = 0x1
	WG_GENL_NAME                                = "wireguard"
	WG_GENL_VERSION                             = 0x1
	WG_KEY_LEN                                  = 0x20
	WIN_ACKMEDIACHANGE                          = 0xdb
	WIN_CHECKPOWERMODE1                         = 0xe5
	WIN_CHECKPOWERMODE2                         = 0x98
	WIN_DEVICE_RESET                            = 0x8
	WIN_DIAGNOSE                                = 0x90
	WIN_DOORLOCK                                = 0xde
	WIN_DOORUNLOCK                              = 0xdf
	WIN_DOWNLOAD_MICROCODE                      = 0x92
	WIN_FLUSH_CACHE                             = 0xe7
	WIN_FLUSH_CACHE_EXT                         = 0xea
	WIN_FORMAT                                  = 0x50
	WIN_GETMEDIASTATUS                          = 0xda
	WIN_IDENTIFY                                = 0xec
	WIN_IDENTIFY_DMA                            = 0xee
	WIN_IDLEIMMEDIATE                           = 0xe1
	WIN_INIT                                    = 0x60
	WIN_MEDIAEJECT                              = 0xed
	WIN_MULTREAD                                = 0xc4
	WIN_MULTREAD_EXT                            = 0x29
	WIN_MULTWRITE                               = 0xc5
	WIN_MULTWRITE_EXT                           = 0x39
	WIN_NOP                                     = 0x0
	WIN_PACKETCMD                               = 0xa0
	WIN_PIDENTIFY                               = 0xa1
	WIN_POSTBOOT                                = 0xdc
	WIN_PREBOOT                                 = 0xdd
	WIN_QUEUED_SERVICE                          = 0xa2
	WIN_READ                                    = 0x20
	WIN_READDMA                                 = 0xc8
	WIN_READDMA_EXT                             = 0x25
	WIN_READDMA_ONCE                            = 0xc9
	WIN_READDMA_QUEUED                          = 0xc7
	WIN_READDMA_QUEUED_EXT                      = 0x26
	WIN_READ_BUFFER                             = 0xe4
	WIN_READ_EXT                                = 0x24
	WIN_READ_LONG                               = 0x22
	WIN_READ_LONG_ONCE                          = 0x23
	WIN_READ_NATIVE_MAX                         = 0xf8
	WIN_READ_NATIVE_MAX_EXT                     = 0x27
	WIN_READ_ONCE                               = 0x21
	WIN_RECAL                                   = 0x10
	WIN_RESTORE                                 = 0x10
	WIN_SECURITY_DISABLE                        = 0xf6
	WIN_SECURITY_ERASE_PREPARE                  = 0xf3
	WIN_SECURITY_ERASE_UNIT                     = 0xf4
	WIN_SECURITY_FREEZE_LOCK                    = 0xf5
	WIN_SECURITY_SET_PASS                       = 0xf1
	WIN_SECURITY_UNLOCK                         = 0xf2
	WIN_SEEK                                    = 0x70
	WIN_SETFEATURES                             = 0xef
	WIN_SETIDLE1                                = 0xe3
	WIN_SETIDLE2                                = 0x97
	WIN_SETMULT                                 = 0xc6
	WIN_SET_MAX                                 = 0xf9
	WIN_SET_MAX_EXT                             = 0x37
	WIN_SLEEPNOW1                               = 0xe6
	WIN_SLEEPNOW2                               = 0x99
	WIN_SMART                                   = 0xb0
	WIN_SPECIFY                                 = 0x91
	WIN_SRST                                    = 0x8
	WIN_STANDBY                                 = 0xe2
	WIN_STANDBY2                                = 0x96
	WIN_STANDBYNOW1                             = 0xe0
	WIN_STANDBYNOW2                             = 0x94
	WIN_VERIFY                                  = 0x40
	WIN_VERIFY_EXT                              = 0x42
	WIN_VERIFY_ONCE                             = 0x41
	WIN_WRITE                                   = 0x30
	WIN_WRITEDMA                                = 0xca
	WIN_WRITEDMA_EXT                            = 0x35
	WIN_WRITEDMA_ONCE                           = 0xcb
	WIN_WRITEDMA_QUEUED                         = 0xcc
	WIN_WRITEDMA_QUEUED_EXT                     = 0x36
	WIN_WRITE_BUFFER                            = 0xe8
	WIN_WRITE_EXT                               = 0x34
	WIN_WRITE_LONG                              = 0x32
	WIN_WRITE_LONG_ONCE                         = 0x33
	WIN_WRITE_ONCE                              = 0x31
	WIN_WRITE_SAME                              = 0xe9
	WIN_WRITE_VERIFY                            = 0x3c
	WNOHANG                                     = 0x1
	WNOTHREAD                                   = 0x20000000
	WNOWAIT                                     = 0x1000000
	WSTOPPED                                    = 0x2
	WUNTRACED                                   = 0x2
	XATTR_CREATE                                = 0x1
	XATTR_REPLACE                               = 0x2
	XDP_COPY                                    = 0x2
	XDP_FLAGS_DRV_MODE                          = 0x4
	XDP_FLAGS_HW_MODE                           = 0x8
	XDP_FLAGS_MASK                              = 0x1f
	XDP_FLAGS_MODES                             = 0xe
	XDP_FLAGS_REPLACE                           = 0x10
	XDP_FLAGS_SKB_MODE                          = 0x2
	XDP_FLAGS_UPDATE_IF_NOEXIST                 = 0x1
	XDP_MMAP_OFFSETS                            = 0x1
	XDP_OPTIONS                                 = 0x8
	XDP_OPTIONS_ZEROCOPY                        = 0x1
	XDP_PACKET_HEADROOM                         = 0x100
	XDP_PGOFF_RX_RING                           = 0x0
	XDP_PGOFF_TX_RING                           = 0x80000000
	XDP_PKT_CONTD                               = 0x1
	XDP_RING_NEED_WAKEUP                        = 0x1
	XDP_RX_RING                                 = 0x2
	XDP_SHARED_UMEM                             = 0x1
	XDP_STATISTICS                              = 0x7
	XDP_TXMD_FLAGS_CHECKSUM                     = 0x2
	XDP_TXMD_FLAGS_LAUNCH_TIME                  = 0x4
	XDP_TXMD_FLAGS_TIMESTAMP                    = 0x1
	XDP_TX_METADATA                             = 0x2
	XDP_TX_RING                                 = 0x3
	XDP_UMEM_COMPLETION_RING                    = 0x6
	XDP_UMEM_FILL_RING                          = 0x5
	XDP_UMEM_PGOFF_COMPLETION_RING              = 0x180000000
	XDP_UMEM_PGOFF_FILL_RING                    = 0x100000000
	XDP_UMEM_REG                                = 0x4
	XDP_UMEM_TX_METADATA_LEN                    = 0x4
	XDP_UMEM_TX_SW_CSUM                         = 0x2
	XDP_UMEM_UNALIGNED_CHUNK_FLAG               = 0x1
	XDP_USE_NEED_WAKEUP                         = 0x8
	XDP_USE_SG                                  = 0x10
	XDP_ZEROCOPY                                = 0x4
	XENFS_SUPER_MAGIC                           = 0xabba1974
	XFS_SUPER_MAGIC                             = 0x58465342
	ZONEFS_MAGIC                                = 0x5a4f4653
	_HIDIOCGRAWNAME_LEN                         = 0x80
	_HIDIOCGRAWPHYS_LEN                         = 0x40
	_HIDIOCGRAWUNIQ_LEN                         = 0x40
)

const (
	SizeofShort    = 0x2
	SizeofInt      = 0x4
	SizeofLongLong = 0x8
	PathMax        = 0x1000
)

type (
	_C_short int16
	_C_int   int32

	_C_long_long int64
)

type ItimerSpec struct {
	Interval Timespec
	Value    Timespec
}

type Itimerval struct {
	Interval Timeval
	Value    Timeval
}

const (
	ADJ_OFFSET            = 0x1
	ADJ_FREQUENCY         = 0x2
	ADJ_MAXERROR          = 0x4
	ADJ_ESTERROR          = 0x8
	ADJ_STATUS            = 0x10
	ADJ_TIMECONST         = 0x20
	ADJ_TAI               = 0x80
	ADJ_SETOFFSET         = 0x100
	ADJ_MICRO             = 0x1000
	ADJ_NANO              = 0x2000
	ADJ_TICK              = 0x4000
	ADJ_OFFSET_SINGLESHOT = 0x8001
	ADJ_OFFSET_SS_READ    = 0xa001
)

const (
	STA_PLL       = 0x1
	STA_PPSFREQ   = 0x2
	STA_PPSTIME   = 0x4
	STA_FLL       = 0x8
	STA_INS       = 0x10
	STA_DEL       = 0x20
	STA_UNSYNC    = 0x40
	STA_FREQHOLD  = 0x80
	STA_PPSSIGNAL = 0x100
	STA_PPSJITTER = 0x200
	STA_PPSWANDER = 0x400
	STA_PPSERROR  = 0x800
	STA_CLOCKERR  = 0x1000
	STA_NANO      = 0x2000
	STA_MODE      = 0x4000
	STA_CLK       = 0x8000
)

const (
	TIME_OK    = 0x0
	TIME_INS   = 0x1
	TIME_DEL   = 0x2
	TIME_OOP   = 0x3
	TIME_WAIT  = 0x4
	TIME_ERROR = 0x5
	TIME_BAD   = 0x5
)

type Rlimit struct {
	Cur uint64
	Max uint64
}

type _Gid_t uint32

type StatxTimestamp struct {
	Sec  int64
	Nsec uint32
	_    int32
}

type Statx_t struct {
	Mask                      uint32
	Blksize                   uint32
	Attributes                uint64
	Nlink                     uint32
	Uid                       uint32
	Gid                       uint32
	Mode                      uint16
	_                         [1]uint16
	Ino                       uint64
	Size                      uint64
	Blocks                    uint64
	Attributes_mask           uint64
	Atime                     StatxTimestamp
	Btime                     StatxTimestamp
	Ctime                     StatxTimestamp
	Mtime                     StatxTimestamp
	Rdev_major                uint32
	Rdev_minor                uint32
	Dev_major                 uint32
	Dev_minor                 uint32
	Mnt_id                    uint64
	Dio_mem_align             uint32
	Dio_offset_align          uint32
	Subvol                    uint64
	Atomic_write_unit_min     uint32
	Atomic_write_unit_max     uint32
	Atomic_write_segments_max uint32
	Dio_read_offset_align     uint32
	Atomic_write_unit_max_opt uint32
	_                         [1]uint32
	_                         [8]uint64
}

type Fsid struct {
	Val [2]int32
}

type FileCloneRange struct {
	Src_fd      int64
	Src_offset  uint64
	Src_length  uint64
	Dest_offset uint64
}

type RawFileDedupeRange struct {
	Src_offset uint64
	Src_length uint64
	Dest_count uint16
	Reserved1  uint16
	Reserved2  uint32
}

type RawFileDedupeRangeInfo struct {
	Dest_fd       int64
	Dest_offset   uint64
	Bytes_deduped uint64
	Status        int32
	Reserved      uint32
}

const (
	SizeofRawFileDedupeRange     = 0x18
	SizeofRawFileDedupeRangeInfo = 0x20
	FILE_DEDUPE_RANGE_SAME       = 0x0
	FILE_DEDUPE_RANGE_DIFFERS    = 0x1
)

type FscryptPolicy struct {
	Version                   uint8
	Contents_encryption_mode  uint8
	Filenames_encryption_mode uint8
	Flags                     uint8
	Master_key_descriptor     [8]uint8
}

type FscryptKey struct {
	Mode uint32
	Raw  [64]uint8
	Size uint32
}

type FscryptPolicyV1 struct {
	Version                   uint8
	Contents_encryption_mode  uint8
	Filenames_encryption_mode uint8
	Flags                     uint8
	Master_key_descriptor     [8]uint8
}

type FscryptPolicyV2 struct {
	Version                   uint8
	Contents_encryption_mode  uint8
	Filenames_encryption_mode uint8
	Flags                     uint8
	Log2_data_unit_size       uint8
	_                         [3]uint8
	Master_key_identifier     [16]uint8
}

type FscryptGetPolicyExArg struct {
	Size   uint64
	Policy [24]byte
}

type FscryptKeySpecifier struct {
	Type uint32
	_    uint32
	U    [32]byte
}

type FscryptAddKeyArg struct {
	Key_spec FscryptKeySpecifier
	Raw_size uint32
	Key_id   uint32
	Flags    uint32
	_        [7]uint32
}

type FscryptRemoveKeyArg struct {
	Key_spec             FscryptKeySpecifier
	Removal_status_flags uint32
	_                    [5]uint32
}

type FscryptGetKeyStatusArg struct {
	Key_spec     FscryptKeySpecifier
	_            [6]uint32
	Status       uint32
	Status_flags uint32
	User_count   uint32
	_            [13]uint32
}

type DmIoctl struct {
	Version      [3]uint32
	Data_size    uint32
	Data_start   uint32
	Target_count uint32
	Open_count   int32
	Flags        uint32
	Event_nr     uint32
	_            uint32
	Dev          uint64
	Name         [128]byte
	Uuid         [129]byte
	Data         [7]byte
}

type DmTargetSpec struct {
	Sector_start uint64
	Length       uint64
	Status       int32
	Next         uint32
	Target_type  [16]byte
}

type DmTargetDeps struct {
	Count uint32
	_     uint32
}

type DmTargetVersions struct {
	Next    uint32
	Version [3]uint32
}

type DmTargetMsg struct {
	Sector uint64
}

const (
	SizeofDmIoctl      = 0x138
	SizeofDmTargetSpec = 0x28
)

type KeyctlDHParams struct {
	Private int32
	Prime   int32
	Base    int32
}

const (
	FADV_NORMAL     = 0x0
	FADV_RANDOM     = 0x1
	FADV_SEQUENTIAL = 0x2
	FADV_WILLNEED   = 0x3
)

type RawSockaddrInet4 struct {
	Family uint16
	Port   uint16
	Addr   [4]byte /* in_addr */
	Zero   [8]uint8
}

type RawSockaddrInet6 struct {
	Family   uint16
	Port     uint16
	Flowinfo uint32
	Addr     [16]byte /* in6_addr */
	Scope_id uint32
}

type RawSockaddrUnix struct {
	Family uint16
	Path   [108]int8
}

type RawSockaddrLinklayer struct {
	Family   uint16
	Protocol uint16
	Ifindex  int32
	Hatype   uint16
	Pkttype  uint8
	Halen    uint8
	Addr     [8]uint8
}

type RawSockaddrNetlink struct {
	Family uint16
	Pad    uint16
	Pid    uint32
	Groups uint32
}

type RawSockaddrHCI struct {
	Family  uint16
	Dev     uint16
	Channel uint16
}

type RawSockaddrL2 struct {
	Family      uint16
	Psm         uint16
	Bdaddr      [6]uint8
	Cid         uint16
	Bdaddr_type uint8
	_           [1]byte
}

type RawSockaddrRFCOMM struct {
	Family  uint16
	Bdaddr  [6]uint8
	Channel uint8
	_       [1]byte
}

type RawSockaddrCAN struct {
	Family  uint16
	Ifindex int32
	Addr    [16]byte
}

type RawSockaddrALG struct {
	Family uint16
	Type   [14]uint8
	Feat   uint32
	Mask   uint32
	Name   [64]uint8
}

type RawSockaddrVM struct {
	Family    uint16
	Reserved1 uint16
	Port      uint32
	Cid       uint32
	Flags     uint8
	Zero      [3]uint8
}

type RawSockaddrXDP struct {
	Family         uint16
	Flags          uint16
	Ifindex        uint32
	Queue_id       uint32
	Shared_umem_fd uint32
}

type RawSockaddrPPPoX [0x1e]byte

type RawSockaddrTIPC struct {
	Family   uint16
	Addrtype uint8
	Scope    int8
	Addr     [12]byte
}

type RawSockaddrL2TPIP struct {
	Family  uint16
	Unused  uint16
	Addr    [4]byte /* in_addr */
	Conn_id uint32
	_       [4]uint8
}

type RawSockaddrL2TPIP6 struct {
	Family   uint16
	Unused   uint16
	Flowinfo uint32
	Addr     [16]byte /* in6_addr */
	Scope_id uint32
	Conn_id  uint32
}

type RawSockaddrIUCV struct {
	Family  uint16
	Port    uint16
	Addr    uint32
	Nodeid  [8]int8
	User_id [8]int8
	Name    [8]int8
}

type RawSockaddrNFC struct {
	Sa_family    uint16
	Dev_idx      uint32
	Target_idx   uint32
	Nfc_protocol uint32
}

type _Socklen uint32

type Linger struct {
	Onoff  int32
	Linger int32
}

type IPMreq struct {
	Multiaddr [4]byte /* in_addr */
	Interface [4]byte /* in_addr */
}

type IPMreqn struct {
	Multiaddr [4]byte /* in_addr */
	Address   [4]byte /* in_addr */
	Ifindex   int32
}

type IPv6Mreq struct {
	Multiaddr [16]byte /* in6_addr */
	Interface uint32
}

type PacketMreq struct {
	Ifindex int32
	Type    uint16
	Alen    uint16
	Address [8]uint8
}

type Inet4Pktinfo struct {
	Ifindex  int32
	Spec_dst [4]byte /* in_addr */
	Addr     [4]byte /* in_addr */
}

type Inet6Pktinfo struct {
	Addr    [16]byte /* in6_addr */
	Ifindex uint32
}

type IPv6MTUInfo struct {
	Addr RawSockaddrInet6
	Mtu  uint32
}

type ICMPv6Filter struct {
	Data [8]uint32
}

type Ucred struct {
	Pid int32
	Uid uint32
	Gid uint32
}

type TCPInfo struct {
	State                uint8
	Ca_state             uint8
	Retransmits          uint8
	Probes               uint8
	Backoff              uint8
	Options              uint8
	Rto                  uint32
	Ato                  uint32
	Snd_mss              uint32
	Rcv_mss              uint32
	Unacked              uint32
	Sacked               uint32
	Lost                 uint32
	Retrans              uint32
	Fackets              uint32
	Last_data_sent       uint32
	Last_ack_sent        uint32
	Last_data_recv       uint32
	Last_ack_recv        uint32
	Pmtu                 uint32
	Rcv_ssthresh         uint32
	Rtt                  uint32
	Rttvar               uint32
	Snd_ssthresh         uint32
	Snd_cwnd             uint32
	Advmss               uint32
	Reordering           uint32
	Rcv_rtt              uint32
	Rcv_space            uint32
	Total_retrans        uint32
	Pacing_rate          uint64
	Max_pacing_rate      uint64
	Bytes_acked          uint64
	Bytes_received       uint64
	Segs_out             uint32
	Segs_in              uint32
	Notsent_bytes        uint32
	Min_rtt              uint32
	Data_segs_in         uint32
	Data_segs_out        uint32
	Delivery_rate        uint64
	Busy_time            uint64
	Rwnd_limited         uint64
	Sndbuf_limited       uint64
	Delivered            uint32
	Delivered_ce         uint32
	Bytes_sent           uint64
	Bytes_retrans        uint64
	Dsack_dups           uint32
	Reord_seen           uint32
	Rcv_ooopack          uint32
	Snd_wnd              uint32
	Rcv_wnd              uint32
	Rehash               uint32
	Total_rto            uint16
	Total_rto_recoveries uint16
	Total_rto_time       uint32
}

type TCPVegasInfo struct {
	Enabled uint32
	Rttcnt  uint32
	Rtt     uint32
	Minrtt  uint32
}

type TCPDCTCPInfo struct {
	Enabled  uint16
	Ce_state uint16
	Alpha    uint32
	Ab_ecn   uint32
	Ab_tot   uint32
}

type TCPBBRInfo struct {
	Bw_lo       uint32
	Bw_hi       uint32
	Min_rtt     uint32
	Pacing_gain uint32
	Cwnd_gain   uint32
}

type CanFilter struct {
	Id   uint32
	Mask uint32
}

type TCPRepairOpt struct {
	Code uint32
	Val  uint32
}

const (
	SizeofSockaddrInet4     = 0x10
	SizeofSockaddrInet6     = 0x1c
	SizeofSockaddrAny       = 0x70
	SizeofSockaddrUnix      = 0x6e
	SizeofSockaddrLinklayer = 0x14
	SizeofSockaddrNetlink   = 0xc
	SizeofSockaddrHCI       = 0x6
	SizeofSockaddrL2        = 0xe
	SizeofSockaddrRFCOMM    = 0xa
	SizeofSockaddrCAN       = 0x18
	SizeofSockaddrALG       = 0x58
	SizeofSockaddrVM        = 0x10
	SizeofSockaddrXDP       = 0x10
	SizeofSockaddrPPPoX     = 0x1e
	SizeofSockaddrTIPC      = 0x10
	SizeofSockaddrL2TPIP    = 0x10
	SizeofSockaddrL2TPIP6   = 0x20
	SizeofSockaddrIUCV      = 0x20
	SizeofSockaddrNFC       = 0x10
	SizeofLinger            = 0x8
	SizeofIPMreq            = 0x8
	SizeofIPMreqn           = 0xc
	SizeofIPv6Mreq          = 0x14
	SizeofPacketMreq        = 0x10
	SizeofInet4Pktinfo      = 0xc
	SizeofInet6Pktinfo      = 0x14
	SizeofIPv6MTUInfo       = 0x20
	SizeofICMPv6Filter      = 0x20
	SizeofUcred             = 0xc
	SizeofTCPInfo           = 0xf8
	SizeofTCPCCInfo         = 0x14
	SizeofCanFilter         = 0x8
	SizeofTCPRepairOpt      = 0x8
)

const (
	NDA_UNSPEC         = 0x0
	NDA_DST            = 0x1
	NDA_LLADDR         = 0x2
	NDA_CACHEINFO      = 0x3
	NDA_PROBES         = 0x4
	NDA_VLAN           = 0x5
	NDA_PORT           = 0x6
	NDA_VNI            = 0x7
	NDA_IFINDEX        = 0x8
	NDA_MASTER         = 0x9
	NDA_LINK_NETNSID   = 0xa
	NDA_SRC_VNI        = 0xb
	NTF_USE            = 0x1
	NTF_SELF           = 0x2
	NTF_MASTER         = 0x4
	NTF_PROXY          = 0x8
	NTF_EXT_LEARNED    = 0x10
	NTF_OFFLOADED      = 0x20
	NTF_ROUTER         = 0x80
	NUD_INCOMPLETE     = 0x1
	NUD_REACHABLE      = 0x2
	NUD_STALE          = 0x4
	NUD_DELAY          = 0x8
	NUD_PROBE          = 0x10
	NUD_FAILED         = 0x20
	NUD_NOARP          = 0x40
	NUD_PERMANENT      = 0x80
	NUD_NONE           = 0x0
	IFA_UNSPEC         = 0x0
	IFA_ADDRESS        = 0x1
	IFA_LOCAL          = 0x2
	IFA_LABEL          = 0x3
	IFA_BROADCAST      = 0x4
	IFA_ANYCAST        = 0x5
	IFA_CACHEINFO      = 0x6
	IFA_MULTICAST      = 0x7
	IFA_FLAGS          = 0x8
	IFA_RT_PRIORITY    = 0x9
	IFA_TARGET_NETNSID = 0xa
	IFAL_LABEL         = 0x2
	IFAL_ADDRESS       = 0x1
	RT_SCOPE_UNIVERSE  = 0x0
	RT_SCOPE_SITE      = 0xc8
	RT_SCOPE_LINK      = 0xfd
	RT_SCOPE_HOST      = 0xfe
	RT_SCOPE_NOWHERE   = 0xff
	RT_TABLE_UNSPEC    = 0x0
	RT_TABLE_COMPAT    = 0xfc
	RT_TABLE_DEFAULT   = 0xfd
	RT_TABLE_MAIN      = 0xfe
	RT_TABLE_LOCAL     = 0xff
	RT_TABLE_MAX       = 0xffffffff
	RTA_UNSPEC         = 0x0
	RTA_DST            = 0x1
	RTA_SRC            = 0x2
	RTA_IIF            = 0x3
	RTA_OIF            = 0x4
	RTA_GATEWAY        = 0x5
	RTA_PRIORITY       = 0x6
	RTA_PREFSRC        = 0x7
	RTA_METRICS        = 0x8
	RTA_MULTIPATH      = 0x9
	RTA_FLOW           = 0xb
	RTA_CACHEINFO      = 0xc
	RTA_TABLE          = 0xf
	RTA_MARK           = 0x10
	RTA_MFC_STATS      = 0x11
	RTA_VIA            = 0x12
	RTA_NEWDST         = 0x13
	RTA_PREF           = 0x14
	RTA_ENCAP_TYPE     = 0x15
	RTA_ENCAP          = 0x16
	RTA_EXPIRES        = 0x17
	RTA_PAD            = 0x18
	RTA_UID            = 0x19
	RTA_TTL_PROPAGATE  = 0x1a
	RTA_IP_PROTO       = 0x1b
	RTA_SPORT          = 0x1c
	RTA_DPORT          = 0x1d
	RTN_UNSPEC         = 0x0
	RTN_UNICAST        = 0x1
	RTN_LOCAL          = 0x2
	RTN_BROADCAST      = 0x3
	RTN_ANYCAST        = 0x4
	RTN_MULTICAST      = 0x5
	RTN_BLACKHOLE      = 0x6
	RTN_UNREACHABLE    = 0x7
	RTN_PROHIBIT       = 0x8
	RTN_THROW          = 0x9
	RTN_NAT            = 0xa
	RTN_XRESOLVE       = 0xb
	SizeofNlMsghdr     = 0x10
	SizeofNlMsgerr     = 0x14
	SizeofRtGenmsg     = 0x1
	SizeofNlAttr       = 0x4
	SizeofRtAttr       = 0x4
	SizeofIfInfomsg    = 0x10
	SizeofIfAddrmsg    = 0x8
	SizeofIfAddrlblmsg = 0xc
	SizeofIfaCacheinfo = 0x10
	SizeofRtMsg        = 0xc
	SizeofRtNexthop    = 0x8
	SizeofNdUseroptmsg = 0x10
	SizeofNdMsg        = 0xc
)

type NlMsghdr struct {
	Len   uint32
	Type  uint16
	Flags uint16
	Seq   uint32
	Pid   uint32
}

type NlMsgerr struct {
	Error int32
	Msg   NlMsghdr
}

type RtGenmsg struct {
	Family uint8
}

type NlAttr struct {
	Len  uint16
	Type uint16
}

type RtAttr struct {
	Len  uint16
	Type uint16
}

type IfInfomsg struct {
	Family uint8
	_      uint8
	Type   uint16
	Index  int32
	Flags  uint32
	Change uint32
}

type IfAddrmsg struct {
	Family    uint8
	Prefixlen uint8
	Flags     uint8
	Scope     uint8
	Index     uint32
}

type IfAddrlblmsg struct {
	Family    uint8
	_         uint8
	Prefixlen uint8
	Flags     uint8
	Index     uint32
	Seq       uint32
}

type IfaCacheinfo struct {
	Prefered uint32
	Valid    uint32
	Cstamp   uint32
	Tstamp   uint32
}

type RtMsg struct {
	Family   uint8
	Dst_len  uint8
	Src_len  uint8
	Tos      uint8
	Table    uint8
	Protocol uint8
	Scope    uint8
	Type     uint8
	Flags    uint32
}

type RtNexthop struct {
	Len     uint16
	Flags   uint8
	Hops    uint8
	Ifindex int32
}

type NdUseroptmsg struct {
	Family    uint8
	Pad1      uint8
	Opts_len  uint16
	Ifindex   int32
	Icmp_type uint8
	Icmp_code uint8
	Pad2      uint16
	Pad3      uint32
}

type NdMsg struct {
	Family  uint8
	Pad1    uint8
	Pad2    uint16
	Ifindex int32
	State   uint16
	Flags   uint8
	Type    uint8
}

const (
	ICMP_FILTER = 0x1

	ICMPV6_FILTER             = 0x1
	ICMPV6_FILTER_BLOCK       = 0x1
	ICMPV6_FILTER_BLOCKOTHERS = 0x3
	ICMPV6_FILTER_PASS        = 0x2
	ICMPV6_FILTER_PASSONLY    = 0x4
)

const (
	SizeofSockFilter = 0x8
)

type SockFilter struct {
	Code uint16
	Jt   uint8
	Jf   uint8
	K    uint32
}

type SockFprog struct {
	Len    uint16
	Filter *SockFilter
}

type InotifyEvent struct {
	Wd     int32
	Mask   uint32
	Cookie uint32
	Len    uint32
}

const SizeofInotifyEvent = 0x10

const SI_LOAD_SHIFT = 0x10

type Utsname struct {
	Sysname    [65]byte
	Nodename   [65]byte
	Release    [65]byte
	Version    [65]byte
	Machine    [65]byte
	Domainname [65]byte
}

const (
	AT_EMPTY_PATH   = 0x1000
	AT_FDCWD        = -0x64
	AT_NO_AUTOMOUNT = 0x800
	AT_REMOVEDIR    = 0x200

	AT_STATX_SYNC_AS_STAT = 0x0
	AT_STATX_FORCE_SYNC   = 0x2000
	AT_STATX_DONT_SYNC    = 0x4000

	AT_RECURSIVE = 0x8000

	AT_SYMLINK_FOLLOW   = 0x400
	AT_SYMLINK_NOFOLLOW = 0x100

	AT_EACCESS = 0x200

	OPEN_TREE_CLONE = 0x1

	MOVE_MOUNT_F_SYMLINKS   = 0x1
	MOVE_MOUNT_F_AUTOMOUNTS = 0x2
	MOVE_MOUNT_F_EMPTY_PATH = 0x4
	MOVE_MOUNT_T_SYMLINKS   = 0x10
	MOVE_MOUNT_T_AUTOMOUNTS = 0x20
	MOVE_MOUNT_T_EMPTY_PATH = 0x40
	MOVE_MOUNT_SET_GROUP    = 0x100

	FSOPEN_CLOEXEC = 0x1

	FSPICK_CLOEXEC          = 0x1
	FSPICK_SYMLINK_NOFOLLOW = 0x2
	FSPICK_NO_AUTOMOUNT     = 0x4
	FSPICK_EMPTY_PATH       = 0x8

	FSMOUNT_CLOEXEC = 0x1

	FSCONFIG_SET_FLAG        = 0x0
	FSCONFIG_SET_STRING      = 0x1
	FSCONFIG_SET_BINARY      = 0x2
	FSCONFIG_SET_PATH        = 0x3
	FSCONFIG_SET_PATH_EMPTY  = 0x4
	FSCONFIG_SET_FD          = 0x5
	FSCONFIG_CMD_CREATE      = 0x6
	FSCONFIG_CMD_RECONFIGURE = 0x7
)

type OpenHow struct {
	Flags   uint64
	Mode    uint64
	Resolve uint64
}

const SizeofOpenHow = 0x18

const (
	RESOLVE_BENEATH       = 0x8
	RESOLVE_IN_ROOT       = 0x10
	RESOLVE_NO_MAGICLINKS = 0x2
	RESOLVE_NO_SYMLINKS   = 0x4
	RESOLVE_NO_XDEV       = 0x1
)

type PollFd struct {
	Fd      int32
	Events  int16
	Revents int16
}

const (
	POLLIN   = 0x1
	POLLPRI  = 0x2
	POLLOUT  = 0x4
	POLLERR  = 0x8
	POLLHUP  = 0x10
	POLLNVAL = 0x20
)

type sigset_argpack struct {
	ss    *Sigset_t
	ssLen uintptr
}

type SignalfdSiginfo struct {
	Signo     uint32
	Errno     int32
	Code      int32
	Pid       uint32
	Uid       uint32
	Fd        int32
	Tid       uint32
	Band      uint32
	Overrun   uint32
	Trapno    uint32
	Status    int32
	Int       int32
	Ptr       uint64
	Utime     uint64
	Stime     uint64
	Addr      uint64
	Addr_lsb  uint16
	_         uint16
	Syscall   int32
	Call_addr uint64
	Arch      uint32
	_         [28]uint8
}

type Winsize struct {
	Row    uint16
	Col    uint16
	Xpixel uint16
	Ypixel uint16
}

const (
	TASKSTATS_CMD_UNSPEC                  = 0x0
	TASKSTATS_CMD_GET                     = 0x1
	TASKSTATS_CMD_NEW                     = 0x2
	TASKSTATS_TYPE_UNSPEC                 = 0x0
	TASKSTATS_TYPE_PID                    = 0x1
	TASKSTATS_TYPE_TGID                   = 0x2
	TASKSTATS_TYPE_STATS                  = 0x3
	TASKSTATS_TYPE_AGGR_PID               = 0x4
	TASKSTATS_TYPE_AGGR_TGID              = 0x5
	TASKSTATS_TYPE_NULL                   = 0x6
	TASKSTATS_CMD_ATTR_UNSPEC             = 0x0
	TASKSTATS_CMD_ATTR_PID                = 0x1
	TASKSTATS_CMD_ATTR_TGID               = 0x2
	TASKSTATS_CMD_ATTR_REGISTER_CPUMASK   = 0x3
	TASKSTATS_CMD_ATTR_DEREGISTER_CPUMASK = 0x4
)

type CGroupStats struct {
	Sleeping        uint64
	Running         uint64
	Stopped         uint64
	Uninterruptible uint64
	Io_wait         uint64
}

const (
	CGROUPSTATS_CMD_UNSPEC        = 0x3
	CGROUPSTATS_CMD_GET           = 0x4
	CGROUPSTATS_CMD_NEW           = 0x5
	CGROUPSTATS_TYPE_UNSPEC       = 0x0
	CGROUPSTATS_TYPE_CGROUP_STATS = 0x1
	CGROUPSTATS_CMD_ATTR_UNSPEC   = 0x0
	CGROUPSTATS_CMD_ATTR_FD       = 0x1
)

type Genlmsghdr struct {
	Cmd      uint8
	Version  uint8
	Reserved uint16
}

const (
	CTRL_CMD_UNSPEC            = 0x0
	CTRL_CMD_NEWFAMILY         = 0x1
	CTRL_CMD_DELFAMILY         = 0x2
	CTRL_CMD_GETFAMILY         = 0x3
	CTRL_CMD_NEWOPS            = 0x4
	CTRL_CMD_DELOPS            = 0x5
	CTRL_CMD_GETOPS            = 0x6
	CTRL_CMD_NEWMCAST_GRP      = 0x7
	CTRL_CMD_DELMCAST_GRP      = 0x8
	CTRL_CMD_GETMCAST_GRP      = 0x9
	CTRL_CMD_GETPOLICY         = 0xa
	CTRL_ATTR_UNSPEC           = 0x0
	CTRL_ATTR_FAMILY_ID        = 0x1
	CTRL_ATTR_FAMILY_NAME      = 0x2
	CTRL_ATTR_VERSION          = 0x3
	CTRL_ATTR_HDRSIZE          = 0x4
	CTRL_ATTR_MAXATTR          = 0x5
	CTRL_ATTR_OPS              = 0x6
	CTRL_ATTR_MCAST_GROUPS     = 0x7
	CTRL_ATTR_POLICY           = 0x8
	CTRL_ATTR_OP_POLICY        = 0x9
	CTRL_ATTR_OP               = 0xa
	CTRL_ATTR_OP_UNSPEC        = 0x0
	CTRL_ATTR_OP_ID            = 0x1
	CTRL_ATTR_OP_FLAGS         = 0x2
	CTRL_ATTR_MCAST_GRP_UNSPEC = 0x0
	CTRL_ATTR_MCAST_GRP_NAME   = 0x1
	CTRL_ATTR_MCAST_GRP_ID     = 0x2
	CTRL_ATTR_POLICY_UNSPEC    = 0x0
	CTRL_ATTR_POLICY_DO        = 0x1
	CTRL_ATTR_POLICY_DUMP      = 0x2
	CTRL_ATTR_POLICY_DUMP_MAX  = 0x2
)

const (
	_CPU_SETSIZE = 0x400
)

const (
	BDADDR_BREDR     = 0x0
	BDADDR_LE_PUBLIC = 0x1
	BDADDR_LE_RANDOM = 0x2
)

type PerfEventAttr struct {
	Type               uint32
	Size               uint32
	Config             uint64
	Sample             uint64
	Sample_type        uint64
	Read_format        uint64
	Bits               uint64
	Wakeup             uint32
	Bp_type            uint32
	Ext1               uint64
	Ext2               uint64
	Branch_sample_type uint64
	Sample_regs_user   uint64
	Sample_stack_user  uint32
	Clockid            int32
	Sample_regs_intr   uint64
	Aux_watermark      uint32
	Sample_max_stack   uint16
	_                  uint16
	Aux_sample_size    uint32
	_                  uint32
	Sig_data           uint64
}

type PerfEventMmapPage struct {
	Version        uint32
	Compat_version uint32
	Lock           uint32
	Index          uint32
	Offset         int64
	Time_enabled   uint64
	Time_running   uint64
	Capabilities   uint64
	Pmc_width      uint16
	Time_shift     uint16
	Time_mult      uint32
	Time_offset    uint64
	Time_zero      uint64
	Size           uint32
	_              uint32
	Time_cycles    uint64
	Time_mask      uint64
	_              [928]uint8
	Data_head      uint64
	Data_tail      uint64
	Data_offset    uint64
	Data_size      uint64
	Aux_head       uint64
	Aux_tail       uint64
	Aux_offset     uint64
	Aux_size       uint64
}

const (
	PerfBitDisabled               uint64 = CBitFieldMaskBit0
	PerfBitInherit                       = CBitFieldMaskBit1
	PerfBitPinned                        = CBitFieldMaskBit2
	PerfBitExclusive                     = CBitFieldMaskBit3
	PerfBitExcludeUser                   = CBitFieldMaskBit4
	PerfBitExcludeKernel                 = CBitFieldMaskBit5
	PerfBitExcludeHv                     = CBitFieldMaskBit6
	PerfBitExcludeIdle                   = CBitFieldMaskBit7
	PerfBitMmap                          = CBitFieldMaskBit8
	PerfBitComm                          = CBitFieldMaskBit9
	PerfBitFreq                          = CBitFieldMaskBit10
	PerfBitInheritStat                   = CBitFieldMaskBit11
	PerfBitEnableOnExec                  = CBitFieldMaskBit12
	PerfBitTask                          = CBitFieldMaskBit13
	PerfBitWatermark                     = CBitFieldMaskBit14
	PerfBitPreciseIPBit1                 = CBitFieldMaskBit15
	PerfBitPreciseIPBit2                 = CBitFieldMaskBit16
	PerfBitMmapData                      = CBitFieldMaskBit17
	PerfBitSampleIDAll                   = CBitFieldMaskBit18
	PerfBitExcludeHost                   = CBitFieldMaskBit19
	PerfBitExcludeGuest                  = CBitFieldMaskBit20
	PerfBitExcludeCallchainKernel        = CBitFieldMaskBit21
	PerfBitExcludeCallchainUser          = CBitFieldMaskBit22
	PerfBitMmap2                         = CBitFieldMaskBit23
	PerfBitCommExec                      = CBitFieldMaskBit24
	PerfBitUseClockID                    = CBitFieldMaskBit25
	PerfBitContextSwitch                 = CBitFieldMaskBit26
	PerfBitWriteBackward                 = CBitFieldMaskBit27
)

const (
	PERF_TYPE_HARDWARE                    = 0x0
	PERF_TYPE_SOFTWARE                    = 0x1
	PERF_TYPE_TRACEPOINT                  = 0x2
	PERF_TYPE_HW_CACHE                    = 0x3
	PERF_TYPE_RAW                         = 0x4
	PERF_TYPE_BREAKPOINT                  = 0x5
	PERF_TYPE_MAX                         = 0x6
	PERF_COUNT_HW_CPU_CYCLES              = 0x0
	PERF_COUNT_HW_INSTRUCTIONS            = 0x1
	PERF_COUNT_HW_CACHE_REFERENCES        = 0x2
	PERF_COUNT_HW_CACHE_MISSES            = 0x3
	PERF_COUNT_HW_BRANCH_INSTRUCTIONS     = 0x4
	PERF_COUNT_HW_BRANCH_MISSES           = 0x5
	PERF_COUNT_HW_BUS_CYCLES              = 0x6
	PERF_COUNT_HW_STALLED_CYCLES_FRONTEND = 0x7
	PERF_COUNT_HW_STALLED_CYCLES_BACKEND  = 0x8
	PERF_COUNT_HW_REF_CPU_CYCLES          = 0x9
	PERF_COUNT_HW_MAX                     = 0xa
	PERF_COUNT_HW_CACHE_L1D               = 0x0
	PERF_COUNT_HW_CACHE_L1I               = 0x1
	PERF_COUNT_HW_CACHE_LL                = 0x2
	PERF_COUNT_HW_CACHE_DTLB              = 0x3
	PERF_COUNT_HW_CACHE_ITLB              = 0x4
	PERF_COUNT_HW_CACHE_BPU               = 0x5
	PERF_COUNT_HW_CACHE_NODE              = 0x6
	PERF_COUNT_HW_CACHE_MAX               = 0x7
	PERF_COUNT_HW_CACHE_OP_READ           = 0x0
	PERF_COUNT_HW_CACHE_OP_WRITE          = 0x1
	PERF_COUNT_HW_CACHE_OP_PREFETCH       = 0x2
	PERF_COUNT_HW_CACHE_OP_MAX            = 0x3
	PERF_COUNT_HW_CACHE_RESULT_ACCESS     = 0x0
	PERF_COUNT_HW_CACHE_RESULT_MISS       = 0x1
	PERF_COUNT_HW_CACHE_RESULT_MAX        = 0x2
	PERF_COUNT_SW_CPU_CLOCK               = 0x0
	PERF_COUNT_SW_TASK_CLOCK              = 0x1
	PERF_COUNT_SW_PAGE_FAULTS             = 0x2
	PERF_COUNT_SW_CONTEXT_SWITCHES        = 0x3
	PERF_COUNT_SW_CPU_MIGRATIONS          = 0x4
	PERF_COUNT_SW_PAGE_FAULTS_MIN         = 0x5
	PERF_COUNT_SW_PAGE_FAULTS_MAJ         = 0x6
	PERF_COUNT_SW_ALIGNMENT_FAULTS        = 0x7
	PERF_COUNT_SW_EMULATION_FAULTS        = 0x8
	PERF_COUNT_SW_DUMMY                   = 0x9
	PERF_COUNT_SW_BPF_OUTPUT              = 0xa
	PERF_COUNT_SW_MAX                     = 0xc
	PERF_SAMPLE_IP                        = 0x1
	PERF_SAMPLE_TID                       = 0x2
	PERF_SAMPLE_TIME                      = 0x4
	PERF_SAMPLE_ADDR                      = 0x8
	PERF_SAMPLE_READ                      = 0x10
	PERF_SAMPLE_CALLCHAIN                 = 0x20
	PERF_SAMPLE_ID                        = 0x40
	PERF_SAMPLE_CPU                       = 0x80
	PERF_SAMPLE_PERIOD                    = 0x100
	PERF_SAMPLE_STREAM_ID                 = 0x200
	PERF_SAMPLE_RAW                       = 0x400
	PERF_SAMPLE_BRANCH_STACK              = 0x800
	PERF_SAMPLE_REGS_USER                 = 0x1000
	PERF_SAMPLE_STACK_USER                = 0x2000
	PERF_SAMPLE_WEIGHT                    = 0x4000
	PERF_SAMPLE_DATA_SRC                  = 0x8000
	PERF_SAMPLE_IDENTIFIER                = 0x10000
	PERF_SAMPLE_TRANSACTION               = 0x20000
	PERF_SAMPLE_REGS_INTR                 = 0x40000
	PERF_SAMPLE_PHYS_ADDR                 = 0x80000
	PERF_SAMPLE_AUX                       = 0x100000
	PERF_SAMPLE_CGROUP                    = 0x200000
	PERF_SAMPLE_DATA_PAGE_SIZE            = 0x400000
	PERF_SAMPLE_CODE_PAGE_SIZE            = 0x800000
	PERF_SAMPLE_WEIGHT_STRUCT             = 0x1000000
	PERF_SAMPLE_MAX                       = 0x2000000
	PERF_SAMPLE_BRANCH_USER_SHIFT         = 0x0
	PERF_SAMPLE_BRANCH_KERNEL_SHIFT       = 0x1
	PERF_SAMPLE_BRANCH_HV_SHIFT           = 0x2
	PERF_SAMPLE_BRANCH_ANY_SHIFT          = 0x3
	PERF_SAMPLE_BRANCH_ANY_CALL_SHIFT     = 0x4
	PERF_SAMPLE_BRANCH_ANY_RETURN_SHIFT   = 0x5
	PERF_SAMPLE_BRANCH_IND_CALL_SHIFT     = 0x6
	PERF_SAMPLE_BRANCH_ABORT_TX_SHIFT     = 0x7
	PERF_SAMPLE_BRANCH_IN_TX_SHIFT        = 0x8
	PERF_SAMPLE_BRANCH_NO_TX_SHIFT        = 0x9
	PERF_SAMPLE_BRANCH_COND_SHIFT         = 0xa
	PERF_SAMPLE_BRANCH_CALL_STACK_SHIFT   = 0xb
	PERF_SAMPLE_BRANCH_IND_JUMP_SHIFT     = 0xc
	PERF_SAMPLE_BRANCH_CALL_SHIFT         = 0xd
	PERF_SAMPLE_BRANCH_NO_FLAGS_SHIFT     = 0xe
	PERF_SAMPLE_BRANCH_NO_CYCLES_SHIFT    = 0xf
	PERF_SAMPLE_BRANCH_TYPE_SAVE_SHIFT    = 0x10
	PERF_SAMPLE_BRANCH_HW_INDEX_SHIFT     = 0x11
	PERF_SAMPLE_BRANCH_PRIV_SAVE_SHIFT    = 0x12
	PERF_SAMPLE_BRANCH_COUNTERS           = 0x80000
	PERF_SAMPLE_BRANCH_MAX_SHIFT          = 0x14
	PERF_SAMPLE_BRANCH_USER               = 0x1
	PERF_SAMPLE_BRANCH_KERNEL             = 0x2
	PERF_SAMPLE_BRANCH_HV                 = 0x4
	PERF_SAMPLE_BRANCH_ANY                = 0x8
	PERF_SAMPLE_BRANCH_ANY_CALL           = 0x10
	PERF_SAMPLE_BRANCH_ANY_RETURN         = 0x20
	PERF_SAMPLE_BRANCH_IND_CALL           = 0x40
	PERF_SAMPLE_BRANCH_ABORT_TX           = 0x80
	PERF_SAMPLE_BRANCH_IN_TX              = 0x100
	PERF_SAMPLE_BRANCH_NO_TX              = 0x200
	PERF_SAMPLE_BRANCH_COND               = 0x400
	PERF_SAMPLE_BRANCH_CALL_STACK         = 0x800
	PERF_SAMPLE_BRANCH_IND_JUMP           = 0x1000
	PERF_SAMPLE_BRANCH_CALL               = 0x2000
	PERF_SAMPLE_BRANCH_NO_FLAGS           = 0x4000
	PERF_SAMPLE_BRANCH_NO_CYCLES          = 0x8000
	PERF_SAMPLE_BRANCH_TYPE_SAVE          = 0x10000
	PERF_SAMPLE_BRANCH_HW_INDEX           = 0x20000
	PERF_SAMPLE_BRANCH_PRIV_SAVE          = 0x40000
	PERF_SAMPLE_BRANCH_MAX                = 0x100000
	PERF_BR_UNKNOWN                       = 0x0
	PERF_BR_COND                          = 0x1
	PERF_BR_UNCOND                        = 0x2
	PERF_BR_IND                           = 0x3
	PERF_BR_CALL                          = 0x4
	PERF_BR_IND_CALL                      = 0x5
	PERF_BR_RET                           = 0x6
	PERF_BR_SYSCALL                       = 0x7
	PERF_BR_SYSRET                        = 0x8
	PERF_BR_COND_CALL                     = 0x9
	PERF_BR_COND_RET                      = 0xa
	PERF_BR_ERET                          = 0xb
	PERF_BR_IRQ                           = 0xc
	PERF_BR_SERROR                        = 0xd
	PERF_BR_NO_TX                         = 0xe
	PERF_BR_EXTEND_ABI                    = 0xf
	PERF_BR_MAX                           = 0x10
	PERF_SAMPLE_REGS_ABI_NONE             = 0x0
	PERF_SAMPLE_REGS_ABI_32               = 0x1
	PERF_SAMPLE_REGS_ABI_64               = 0x2
	PERF_TXN_ELISION                      = 0x1
	PERF_TXN_TRANSACTION                  = 0x2
	PERF_TXN_SYNC                         = 0x4
	PERF_TXN_ASYNC                        = 0x8
	PERF_TXN_RETRY                        = 0x10
	PERF_TXN_CONFLICT                     = 0x20
	PERF_TXN_CAPACITY_WRITE               = 0x40
	PERF_TXN_CAPACITY_READ                = 0x80
	PERF_TXN_MAX                          = 0x100
	PERF_TXN_ABORT_MASK                   = -0x100000000
	PERF_TXN_ABORT_SHIFT                  = 0x20
	PERF_FORMAT_TOTAL_TIME_ENABLED        = 0x1
	PERF_FORMAT_TOTAL_TIME_RUNNING        = 0x2
	PERF_FORMAT_ID                        = 0x4
	PERF_FORMAT_GROUP                     = 0x8
	PERF_FORMAT_LOST                      = 0x10
	PERF_FORMAT_MAX                       = 0x20
	PERF_IOC_FLAG_GROUP                   = 0x1
	PERF_RECORD_MMAP                      = 0x1
	PERF_RECORD_LOST                      = 0x2
	PERF_RECORD_COMM                      = 0x3
	PERF_RECORD_EXIT                      = 0x4
	PERF_RECORD_THROTTLE                  = 0x5
	PERF_RECORD_UNTHROTTLE                = 0x6
	PERF_RECORD_FORK                      = 0x7
	PERF_RECORD_READ                      = 0x8
	PERF_RECORD_SAMPLE                    = 0x9
	PERF_RECORD_MMAP2                     = 0xa
	PERF_RECORD_AUX                       = 0xb
	PERF_RECORD_ITRACE_START              = 0xc
	PERF_RECORD_LOST_SAMPLES              = 0xd
	PERF_RECORD_SWITCH                    = 0xe
	PERF_RECORD_SWITCH_CPU_WIDE           = 0xf
	PERF_RECORD_NAMESPACES                = 0x10
	PERF_RECORD_KSYMBOL                   = 0x11
	PERF_RECORD_BPF_EVENT                 = 0x12
	PERF_RECORD_CGROUP                    = 0x13
	PERF_RECORD_TEXT_POKE                 = 0x14
	PERF_RECORD_AUX_OUTPUT_HW_ID          = 0x15
	PERF_RECORD_MAX                       = 0x16
	PERF_RECORD_KSYMBOL_TYPE_UNKNOWN      = 0x0
	PERF_RECORD_KSYMBOL_TYPE_BPF          = 0x1
	PERF_RECORD_KSYMBOL_TYPE_OOL          = 0x2
	PERF_RECORD_KSYMBOL_TYPE_MAX          = 0x3
	PERF_BPF_EVENT_UNKNOWN                = 0x0
	PERF_BPF_EVENT_PROG_LOAD              = 0x1
	PERF_BPF_EVENT_PROG_UNLOAD            = 0x2
	PERF_BPF_EVENT_MAX                    = 0x3
	PERF_CONTEXT_HV                       = -0x20
	PERF_CONTEXT_KERNEL                   = -0x80
	PERF_CONTEXT_USER                     = -0x200
	PERF_CONTEXT_GUEST                    = -0x800
	PERF_CONTEXT_GUEST_KERNEL             = -0x880
	PERF_CONTEXT_GUEST_USER               = -0xa00
	PERF_CONTEXT_MAX                      = -0xfff
)

type TCPMD5Sig struct {
	Addr      SockaddrStorage
	Flags     uint8
	Prefixlen uint8
	Keylen    uint16
	Ifindex   int32
	Key       [80]uint8
}

type HDDriveCmdHdr struct {
	Command uint8
	Number  uint8
	Feature uint8
	Count   uint8
}

type HDDriveID struct {
	Config         uint16
	Cyls           uint16
	Reserved2      uint16
	Heads          uint16
	Track_bytes    uint16
	Sector_bytes   uint16
	Sectors        uint16
	Vendor0        uint16
	Vendor1        uint16
	Vendor2        uint16
	Serial_no      [20]uint8
	Buf_type       uint16
	Buf_size       uint16
	Ecc_bytes      uint16
	Fw_rev         [8]uint8
	Model          [40]uint8
	Max_multsect   uint8
	Vendor3        uint8
	Dword_io       uint16
	Vendor4        uint8
	Capability     uint8
	Reserved50     uint16
	Vendor5        uint8
	TPIO           uint8
	Vendor6        uint8
	TDMA           uint8
	Field_valid    uint16
	Cur_cyls       uint16
	Cur_heads      uint16
	Cur_sectors    uint16
	Cur_capacity0  uint16
	Cur_capacity1  uint16
	Multsect       uint8
	Multsect_valid uint8
	Lba_capacity   uint32
	Dma_1word      uint16
	Dma_mword      uint16
	Eide_pio_modes uint16
	Eide_dma_min   uint16
	Eide_dma_time  uint16
	Eide_pio       uint16
	Eide_pio_iordy uint16
	Words69_70     [2]uint16
	Words71_74     [4]uint16
	Queue_depth    uint16
	Words76_79     [4]uint16
	Major_rev_num  uint16
	Minor_rev_num  uint16
	Command_set_1  uint16
	Command_set_2  uint16
	Cfsse          uint16
	Cfs_enable_1   uint16
	Cfs_enable_2   uint16
	Csf_default    uint16
	Dma_ultra      uint16
	Trseuc         uint16
	TrsEuc         uint16
	CurAPMvalues   uint16
	Mprc           uint16
	Hw_config      uint16
	Acoustic       uint16
	Msrqs          uint16
	Sxfert         uint16
	Sal            uint16
	Spg            uint32
	Lba_capacity_2 uint64
	Words104_125   [22]uint16
	Last_lun       uint16
	Word127        uint16
	Dlf            uint16
	Csfo           uint16
	Words130_155   [26]uint16
	Word156        uint16
	Words157_159   [3]uint16
	Cfa_power      uint16
	Words161_175   [15]uint16
	Words176_205   [30]uint16
	Words206_254   [49]uint16
	Integrity_word uint16
}

const (
	ST_MANDLOCK    = 0x40
	ST_NOATIME     = 0x400
	ST_NODEV       = 0x4
	ST_NODIRATIME  = 0x800
	ST_NOEXEC      = 0x8
	ST_NOSUID      = 0x2
	ST_RDONLY      = 0x1
	ST_RELATIME    = 0x1000
	ST_SYNCHRONOUS = 0x10
)

type Tpacket2Hdr struct {
	Status    uint32
	Len       uint32
	Snaplen   uint32
	Mac       uint16
	Net       uint16
	Sec       uint32
	Nsec      uint32
	Vlan_tci  uint16
	Vlan_tpid uint16
	_         [4]uint8
}

type Tpacket3Hdr struct {
	Next_offset uint32
	Sec         uint32
	Nsec        uint32
	Snaplen     uint32
	Len         uint32
	Status      uint32
	Mac         uint16
	Net         uint16
	Hv1         TpacketHdrVariant1
	_           [8]uint8
}

type TpacketHdrVariant1 struct {
	Rxhash    uint32
	Vlan_tci  uint32
	Vlan_tpid uint16
	_         uint16
}

type TpacketBlockDesc struct {
	Version uint32
	To_priv uint32
	Hdr     [40]byte
}

type TpacketBDTS struct {
	Sec  uint32
	Usec uint32
}

type TpacketHdrV1 struct {
	Block_status        uint32
	Num_pkts            uint32
	Offset_to_first_pkt uint32
	Blk_len             uint32
	Seq_num             uint64
	Ts_first_pkt        TpacketBDTS
	Ts_last_pkt         TpacketBDTS
}

type TpacketReq struct {
	Block_size uint32
	Block_nr   uint32
	Frame_size uint32
	Frame_nr   uint32
}

type TpacketReq3 struct {
	Block_size       uint32
	Block_nr         uint32
	Frame_size       uint32
	Frame_nr         uint32
	Retire_blk_tov   uint32
	Sizeof_priv      uint32
	Feature_req_word uint32
}

type TpacketStats struct {
	Packets uint32
	Drops   uint32
}

type TpacketStatsV3 struct {
	Packets      uint32
	Drops        uint32
	Freeze_q_cnt uint32
}

type TpacketAuxdata struct {
	Status    uint32
	Len       uint32
	Snaplen   uint32
	Mac       uint16
	Net       uint16
	Vlan_tci  uint16
	Vlan_tpid uint16
}

const (
	TPACKET_V1 = 0x0
	TPACKET_V2 = 0x1
	TPACKET_V3 = 0x2
)

const (
	SizeofTpacket2Hdr = 0x20
	SizeofTpacket3Hdr = 0x30

	SizeofTpacketStats   = 0x8
	SizeofTpacketStatsV3 = 0xc
)

const (
	IFLA_UNSPEC                                = 0x0
	IFLA_ADDRESS                               = 0x1
	IFLA_BROADCAST                             = 0x2
	IFLA_IFNAME                                = 0x3
	IFLA_MTU                                   = 0x4
	IFLA_LINK                                  = 0x5
	IFLA_QDISC                                 = 0x6
	IFLA_STATS                                 = 0x7
	IFLA_COST                                  = 0x8
	IFLA_PRIORITY                              = 0x9
	IFLA_MASTER                                = 0xa
	IFLA_WIRELESS                              = 0xb
	IFLA_PROTINFO                              = 0xc
	IFLA_TXQLEN                                = 0xd
	IFLA_MAP                                   = 0xe
	IFLA_WEIGHT                                = 0xf
	IFLA_OPERSTATE                             = 0x10
	IFLA_LINKMODE                              = 0x11
	IFLA_LINKINFO                              = 0x12
	IFLA_NET_NS_PID                            = 0x13
	IFLA_IFALIAS                               = 0x14
	IFLA_NUM_VF                                = 0x15
	IFLA_VFINFO_LIST                           = 0x16
	IFLA_STATS64                               = 0x17
	IFLA_VF_PORTS                              = 0x18
	IFLA_PORT_SELF                             = 0x19
	IFLA_AF_SPEC                               = 0x1a
	IFLA_GROUP                                 = 0x1b
	IFLA_NET_NS_FD                             = 0x1c
	IFLA_EXT_MASK                              = 0x1d
	IFLA_PROMISCUITY                           = 0x1e
	IFLA_NUM_TX_QUEUES                         = 0x1f
	IFLA_NUM_RX_QUEUES                         = 0x20
	IFLA_CARRIER                               = 0x21
	IFLA_PHYS_PORT_ID                          = 0x22
	IFLA_CARRIER_CHANGES                       = 0x23
	IFLA_PHYS_SWITCH_ID                        = 0x24
	IFLA_LINK_NETNSID                          = 0x25
	IFLA_PHYS_PORT_NAME                        = 0x26
	IFLA_PROTO_DOWN                            = 0x27
	IFLA_GSO_MAX_SEGS                          = 0x28
	IFLA_GSO_MAX_SIZE                          = 0x29
	IFLA_PAD                                   = 0x2a
	IFLA_XDP                                   = 0x2b
	IFLA_EVENT                                 = 0x2c
	IFLA_NEW_NETNSID                           = 0x2d
	IFLA_IF_NETNSID                            = 0x2e
	IFLA_TARGET_NETNSID                        = 0x2e
	IFLA_CARRIER_UP_COUNT                      = 0x2f
	IFLA_CARRIER_DOWN_COUNT                    = 0x30
	IFLA_NEW_IFINDEX                           = 0x31
	IFLA_MIN_MTU                               = 0x32
	IFLA_MAX_MTU                               = 0x33
	IFLA_PROP_LIST                             = 0x34
	IFLA_ALT_IFNAME                            = 0x35
	IFLA_PERM_ADDRESS                          = 0x36
	IFLA_PROTO_DOWN_REASON                     = 0x37
	IFLA_PARENT_DEV_NAME                       = 0x38
	IFLA_PARENT_DEV_BUS_NAME                   = 0x39
	IFLA_GRO_MAX_SIZE                          = 0x3a
	IFLA_TSO_MAX_SIZE                          = 0x3b
	IFLA_TSO_MAX_SEGS                          = 0x3c
	IFLA_ALLMULTI                              = 0x3d
	IFLA_DEVLINK_PORT                          = 0x3e
	IFLA_GSO_IPV4_MAX_SIZE                     = 0x3f
	IFLA_GRO_IPV4_MAX_SIZE                     = 0x40
	IFLA_DPLL_PIN                              = 0x41
	IFLA_PROTO_DOWN_REASON_UNSPEC              = 0x0
	IFLA_PROTO_DOWN_REASON_MASK                = 0x1
	IFLA_PROTO_DOWN_REASON_VALUE               = 0x2
	IFLA_PROTO_DOWN_REASON_MAX                 = 0x2
	IFLA_INET_UNSPEC                           = 0x0
	IFLA_INET_CONF                             = 0x1
	IFLA_INET6_UNSPEC                          = 0x0
	IFLA_INET6_FLAGS                           = 0x1
	IFLA_INET6_CONF                            = 0x2
	IFLA_INET6_STATS                           = 0x3
	IFLA_INET6_MCAST                           = 0x4
	IFLA_INET6_CACHEINFO                       = 0x5
	IFLA_INET6_ICMP6STATS                      = 0x6
	IFLA_INET6_TOKEN                           = 0x7
	IFLA_INET6_ADDR_GEN_MODE                   = 0x8
	IFLA_INET6_RA_MTU                          = 0x9
	IFLA_BR_UNSPEC                             = 0x0
	IFLA_BR_FORWARD_DELAY                      = 0x1
	IFLA_BR_HELLO_TIME                         = 0x2
	IFLA_BR_MAX_AGE                            = 0x3
	IFLA_BR_AGEING_TIME                        = 0x4
	IFLA_BR_STP_STATE                          = 0x5
	IFLA_BR_PRIORITY                           = 0x6
	IFLA_BR_VLAN_FILTERING                     = 0x7
	IFLA_BR_VLAN_PROTOCOL                      = 0x8
	IFLA_BR_GROUP_FWD_MASK                     = 0x9
	IFLA_BR_ROOT_ID                            = 0xa
	IFLA_BR_BRIDGE_ID                          = 0xb
	IFLA_BR_ROOT_PORT                          = 0xc
	IFLA_BR_ROOT_PATH_COST                     = 0xd
	IFLA_BR_TOPOLOGY_CHANGE                    = 0xe
	IFLA_BR_TOPOLOGY_CHANGE_DETECTED           = 0xf
	IFLA_BR_HELLO_TIMER                        = 0x10
	IFLA_BR_TCN_TIMER                          = 0x11
	IFLA_BR_TOPOLOGY_CHANGE_TIMER              = 0x12
	IFLA_BR_GC_TIMER                           = 0x13
	IFLA_BR_GROUP_ADDR                         = 0x14
	IFLA_BR_FDB_FLUSH                          = 0x15
	IFLA_BR_MCAST_ROUTER                       = 0x16
	IFLA_BR_MCAST_SNOOPING                     = 0x17
	IFLA_BR_MCAST_QUERY_USE_IFADDR             = 0x18
	IFLA_BR_MCAST_QUERIER                      = 0x19
	IFLA_BR_MCAST_HASH_ELASTICITY              = 0x1a
	IFLA_BR_MCAST_HASH_MAX                     = 0x1b
	IFLA_BR_MCAST_LAST_MEMBER_CNT              = 0x1c
	IFLA_BR_MCAST_STARTUP_QUERY_CNT            = 0x1d
	IFLA_BR_MCAST_LAST_MEMBER_INTVL            = 0x1e
	IFLA_BR_MCAST_MEMBERSHIP_INTVL             = 0x1f
	IFLA_BR_MCAST_QUERIER_INTVL                = 0x20
	IFLA_BR_MCAST_QUERY_INTVL                  = 0x21
	IFLA_BR_MCAST_QUERY_RESPONSE_INTVL         = 0x22
	IFLA_BR_MCAST_STARTUP_QUERY_INTVL          = 0x23
	IFLA_BR_NF_CALL_IPTABLES                   = 0x24
	IFLA_BR_NF_CALL_IP6TABLES                  = 0x25
	IFLA_BR_NF_CALL_ARPTABLES                  = 0x26
	IFLA_BR_VLAN_DEFAULT_PVID                  = 0x27
	IFLA_BR_PAD                                = 0x28
	IFLA_BR_VLAN_STATS_ENABLED                 = 0x29
	IFLA_BR_MCAST_STATS_ENABLED                = 0x2a
	IFLA_BR_MCAST_IGMP_VERSION                 = 0x2b
	IFLA_BR_MCAST_MLD_VERSION                  = 0x2c
	IFLA_BR_VLAN_STATS_PER_PORT                = 0x2d
	IFLA_BR_MULTI_BOOLOPT                      = 0x2e
	IFLA_BR_MCAST_QUERIER_STATE                = 0x2f
	IFLA_BR_FDB_N_LEARNED                      = 0x30
	IFLA_BR_FDB_MAX_LEARNED                    = 0x31
	IFLA_BRPORT_UNSPEC                         = 0x0
	IFLA_BRPORT_STATE                          = 0x1
	IFLA_BRPORT_PRIORITY                       = 0x2
	IFLA_BRPORT_COST                           = 0x3
	IFLA_BRPORT_MODE                           = 0x4
	IFLA_BRPORT_GUARD                          = 0x5
	IFLA_BRPORT_PROTECT                        = 0x6
	IFLA_BRPORT_FAST_LEAVE                     = 0x7
	IFLA_BRPORT_LEARNING                       = 0x8
	IFLA_BRPORT_UNICAST_FLOOD                  = 0x9
	IFLA_BRPORT_PROXYARP                       = 0xa
	IFLA_BRPORT_LEARNING_SYNC                  = 0xb
	IFLA_BRPORT_PROXYARP_WIFI                  = 0xc
	IFLA_BRPORT_ROOT_ID                        = 0xd
	IFLA_BRPORT_BRIDGE_ID                      = 0xe
	IFLA_BRPORT_DESIGNATED_PORT                = 0xf
	IFLA_BRPORT_DESIGNATED_COST                = 0x10
	IFLA_BRPORT_ID                             = 0x11
	IFLA_BRPORT_NO                             = 0x12
	IFLA_BRPORT_TOPOLOGY_CHANGE_ACK            = 0x13
	IFLA_BRPORT_CONFIG_PENDING                 = 0x14
	IFLA_BRPORT_MESSAGE_AGE_TIMER              = 0x15
	IFLA_BRPORT_FORWARD_DELAY_TIMER            = 0x16
	IFLA_BRPORT_HOLD_TIMER                     = 0x17
	IFLA_BRPORT_FLUSH                          = 0x18
	IFLA_BRPORT_MULTICAST_ROUTER               = 0x19
	IFLA_BRPORT_PAD                            = 0x1a
	IFLA_BRPORT_MCAST_FLOOD                    = 0x1b
	IFLA_BRPORT_MCAST_TO_UCAST                 = 0x1c
	IFLA_BRPORT_VLAN_TUNNEL                    = 0x1d
	IFLA_BRPORT_BCAST_FLOOD                    = 0x1e
	IFLA_BRPORT_GROUP_FWD_MASK                 = 0x1f
	IFLA_BRPORT_NEIGH_SUPPRESS                 = 0x20
	IFLA_BRPORT_ISOLATED                       = 0x21
	IFLA_BRPORT_BACKUP_PORT                    = 0x22
	IFLA_BRPORT_MRP_RING_OPEN                  = 0x23
	IFLA_BRPORT_MRP_IN_OPEN                    = 0x24
	IFLA_BRPORT_MCAST_EHT_HOSTS_LIMIT          = 0x25
	IFLA_BRPORT_MCAST_EHT_HOSTS_CNT            = 0x26
	IFLA_BRPORT_LOCKED                         = 0x27
	IFLA_BRPORT_MAB                            = 0x28
	IFLA_BRPORT_MCAST_N_GROUPS                 = 0x29
	IFLA_BRPORT_MCAST_MAX_GROUPS               = 0x2a
	IFLA_BRPORT_NEIGH_VLAN_SUPPRESS            = 0x2b
	IFLA_BRPORT_BACKUP_NHID                    = 0x2c
	IFLA_INFO_UNSPEC                           = 0x0
	IFLA_INFO_KIND                             = 0x1
	IFLA_INFO_DATA                             = 0x2
	IFLA_INFO_XSTATS                           = 0x3
	IFLA_INFO_SLAVE_KIND                       = 0x4
	IFLA_INFO_SLAVE_DATA                       = 0x5
	IFLA_VLAN_UNSPEC                           = 0x0
	IFLA_VLAN_ID                               = 0x1
	IFLA_VLAN_FLAGS                            = 0x2
	IFLA_VLAN_EGRESS_QOS                       = 0x3
	IFLA_VLAN_INGRESS_QOS                      = 0x4
	IFLA_VLAN_PROTOCOL                         = 0x5
	IFLA_VLAN_QOS_UNSPEC                       = 0x0
	IFLA_VLAN_QOS_MAPPING                      = 0x1
	IFLA_MACVLAN_UNSPEC                        = 0x0
	IFLA_MACVLAN_MODE                          = 0x1
	IFLA_MACVLAN_FLAGS                         = 0x2
	IFLA_MACVLAN_MACADDR_MODE                  = 0x3
	IFLA_MACVLAN_MACADDR                       = 0x4
	IFLA_MACVLAN_MACADDR_DATA                  = 0x5
	IFLA_MACVLAN_MACADDR_COUNT                 = 0x6
	IFLA_MACVLAN_BC_QUEUE_LEN                  = 0x7
	IFLA_MACVLAN_BC_QUEUE_LEN_USED             = 0x8
	IFLA_MACVLAN_BC_CUTOFF                     = 0x9
	IFLA_VRF_UNSPEC                            = 0x0
	IFLA_VRF_TABLE                             = 0x1
	IFLA_VRF_PORT_UNSPEC                       = 0x0
	IFLA_VRF_PORT_TABLE                        = 0x1
	IFLA_MACSEC_UNSPEC                         = 0x0
	IFLA_MACSEC_SCI                            = 0x1
	IFLA_MACSEC_PORT                           = 0x2
	IFLA_MACSEC_ICV_LEN                        = 0x3
	IFLA_MACSEC_CIPHER_SUITE                   = 0x4
	IFLA_MACSEC_WINDOW                         = 0x5
	IFLA_MACSEC_ENCODING_SA                    = 0x6
	IFLA_MACSEC_ENCRYPT                        = 0x7
	IFLA_MACSEC_PROTECT                        = 0x8
	IFLA_MACSEC_INC_SCI                        = 0x9
	IFLA_MACSEC_ES                             = 0xa
	IFLA_MACSEC_SCB                            = 0xb
	IFLA_MACSEC_REPLAY_PROTECT                 = 0xc
	IFLA_MACSEC_VALIDATION                     = 0xd
	IFLA_MACSEC_PAD                            = 0xe
	IFLA_MACSEC_OFFLOAD                        = 0xf
	IFLA_XFRM_UNSPEC                           = 0x0
	IFLA_XFRM_LINK                             = 0x1
	IFLA_XFRM_IF_ID                            = 0x2
	IFLA_XFRM_COLLECT_METADATA                 = 0x3
	IFLA_IPVLAN_UNSPEC                         = 0x0
	IFLA_IPVLAN_MODE                           = 0x1
	IFLA_IPVLAN_FLAGS                          = 0x2
	IFLA_NETKIT_UNSPEC                         = 0x0
	IFLA_NETKIT_PEER_INFO                      = 0x1
	IFLA_NETKIT_PRIMARY                        = 0x2
	IFLA_NETKIT_POLICY                         = 0x3
	IFLA_NETKIT_PEER_POLICY                    = 0x4
	IFLA_NETKIT_MODE                           = 0x5
	IFLA_VXLAN_UNSPEC                          = 0x0
	IFLA_VXLAN_ID                              = 0x1
	IFLA_VXLAN_GROUP                           = 0x2
	IFLA_VXLAN_LINK                            = 0x3
	IFLA_VXLAN_LOCAL                           = 0x4
	IFLA_VXLAN_TTL                             = 0x5
	IFLA_VXLAN_TOS                             = 0x6
	IFLA_VXLAN_LEARNING                        = 0x7
	IFLA_VXLAN_AGEING                          = 0x8
	IFLA_VXLAN_LIMIT                           = 0x9
	IFLA_VXLAN_PORT_RANGE                      = 0xa
	IFLA_VXLAN_PROXY                           = 0xb
	IFLA_VXLAN_RSC                             = 0xc
	IFLA_VXLAN_L2MISS                          = 0xd
	IFLA_VXLAN_L3MISS                          = 0xe
	IFLA_VXLAN_PORT                            = 0xf
	IFLA_VXLAN_GROUP6                          = 0x10
	IFLA_VXLAN_LOCAL6                          = 0x11
	IFLA_VXLAN_UDP_CSUM                        = 0x12
	IFLA_VXLAN_UDP_ZERO_CSUM6_TX               = 0x13
	IFLA_VXLAN_UDP_ZERO_CSUM6_RX               = 0x14
	IFLA_VXLAN_REMCSUM_TX                      = 0x15
	IFLA_VXLAN_REMCSUM_RX                      = 0x16
	IFLA_VXLAN_GBP                             = 0x17
	IFLA_VXLAN_REMCSUM_NOPARTIAL               = 0x18
	IFLA_VXLAN_COLLECT_METADATA                = 0x19
	IFLA_VXLAN_LABEL                           = 0x1a
	IFLA_VXLAN_GPE                             = 0x1b
	IFLA_VXLAN_TTL_INHERIT                     = 0x1c
	IFLA_VXLAN_DF                              = 0x1d
	IFLA_VXLAN_VNIFILTER                       = 0x1e
	IFLA_VXLAN_LOCALBYPASS                     = 0x1f
	IFLA_VXLAN_LABEL_POLICY                    = 0x20
	IFLA_GENEVE_UNSPEC                         = 0x0
	IFLA_GENEVE_ID                             = 0x1
	IFLA_GENEVE_REMOTE                         = 0x2
	IFLA_GENEVE_TTL                            = 0x3
	IFLA_GENEVE_TOS                            = 0x4
	IFLA_GENEVE_PORT                           = 0x5
	IFLA_GENEVE_COLLECT_METADATA               = 0x6
	IFLA_GENEVE_REMOTE6                        = 0x7
	IFLA_GENEVE_UDP_CSUM                       = 0x8
	IFLA_GENEVE_UDP_ZERO_CSUM6_TX              = 0x9
	IFLA_GENEVE_UDP_ZERO_CSUM6_RX              = 0xa
	IFLA_GENEVE_LABEL                          = 0xb
	IFLA_GENEVE_TTL_INHERIT                    = 0xc
	IFLA_GENEVE_DF                             = 0xd
	IFLA_GENEVE_INNER_PROTO_INHERIT            = 0xe
	IFLA_BAREUDP_UNSPEC                        = 0x0
	IFLA_BAREUDP_PORT                          = 0x1
	IFLA_BAREUDP_ETHERTYPE                     = 0x2
	IFLA_BAREUDP_SRCPORT_MIN                   = 0x3
	IFLA_BAREUDP_MULTIPROTO_MODE               = 0x4
	IFLA_PPP_UNSPEC                            = 0x0
	IFLA_PPP_DEV_FD                            = 0x1
	IFLA_GTP_UNSPEC                            = 0x0
	IFLA_GTP_FD0                               = 0x1
	IFLA_GTP_FD1                               = 0x2
	IFLA_GTP_PDP_HASHSIZE                      = 0x3
	IFLA_GTP_ROLE                              = 0x4
	IFLA_GTP_CREATE_SOCKETS                    = 0x5
	IFLA_GTP_RESTART_COUNT                     = 0x6
	IFLA_GTP_LOCAL                             = 0x7
	IFLA_GTP_LOCAL6                            = 0x8
	IFLA_BOND_UNSPEC                           = 0x0
	IFLA_BOND_MODE                             = 0x1
	IFLA_BOND_ACTIVE_SLAVE                     = 0x2
	IFLA_BOND_MIIMON                           = 0x3
	IFLA_BOND_UPDELAY                          = 0x4
	IFLA_BOND_DOWNDELAY                        = 0x5
	IFLA_BOND_USE_CARRIER                      = 0x6
	IFLA_BOND_ARP_INTERVAL                     = 0x7
	IFLA_BOND_ARP_IP_TARGET                    = 0x8
	IFLA_BOND_ARP_VALIDATE                     = 0x9
	IFLA_BOND_ARP_ALL_TARGETS                  = 0xa
	IFLA_BOND_PRIMARY                          = 0xb
	IFLA_BOND_PRIMARY_RESELECT                 = 0xc
	IFLA_BOND_FAIL_OVER_MAC                    = 0xd
	IFLA_BOND_XMIT_HASH_POLICY                 = 0xe
	IFLA_BOND_RESEND_IGMP                      = 0xf
	IFLA_BOND_NUM_PEER_NOTIF                   = 0x10
	IFLA_BOND_ALL_SLAVES_ACTIVE                = 0x11
	IFLA_BOND_MIN_LINKS                        = 0x12
	IFLA_BOND_LP_INTERVAL                      = 0x13
	IFLA_BOND_PACKETS_PER_SLAVE                = 0x14
	IFLA_BOND_AD_LACP_RATE                     = 0x15
	IFLA_BOND_AD_SELECT                        = 0x16
	IFLA_BOND_AD_INFO                          = 0x17
	IFLA_BOND_AD_ACTOR_SYS_PRIO                = 0x18
	IFLA_BOND_AD_USER_PORT_KEY                 = 0x19
	IFLA_BOND_AD_ACTOR_SYSTEM                  = 0x1a
	IFLA_BOND_TLB_DYNAMIC_LB                   = 0x1b
	IFLA_BOND_PEER_NOTIF_DELAY                 = 0x1c
	IFLA_BOND_AD_LACP_ACTIVE                   = 0x1d
	IFLA_BOND_MISSED_MAX                       = 0x1e
	IFLA_BOND_NS_IP6_TARGET                    = 0x1f
	IFLA_BOND_COUPLED_CONTROL                  = 0x20
	IFLA_BOND_AD_INFO_UNSPEC                   = 0x0
	IFLA_BOND_AD_INFO_AGGREGATOR               = 0x1
	IFLA_BOND_AD_INFO_NUM_PORTS                = 0x2
	IFLA_BOND_AD_INFO_ACTOR_KEY                = 0x3
	IFLA_BOND_AD_INFO_PARTNER_KEY              = 0x4
	IFLA_BOND_AD_INFO_PARTNER_MAC              = 0x5
	IFLA_BOND_SLAVE_UNSPEC                     = 0x0
	IFLA_BOND_SLAVE_STATE                      = 0x1
	IFLA_BOND_SLAVE_MII_STATUS                 = 0x2
	IFLA_BOND_SLAVE_LINK_FAILURE_COUNT         = 0x3
	IFLA_BOND_SLAVE_PERM_HWADDR                = 0x4
	IFLA_BOND_SLAVE_QUEUE_ID                   = 0x5
	IFLA_BOND_SLAVE_AD_AGGREGATOR_ID           = 0x6
	IFLA_BOND_SLAVE_AD_ACTOR_OPER_PORT_STATE   = 0x7
	IFLA_BOND_SLAVE_AD_PARTNER_OPER_PORT_STATE = 0x8
	IFLA_BOND_SLAVE_PRIO                       = 0x9
	IFLA_VF_INFO_UNSPEC                        = 0x0
	IFLA_VF_INFO                               = 0x1
	IFLA_VF_UNSPEC                             = 0x0
	IFLA_VF_MAC                                = 0x1
	IFLA_VF_VLAN                               = 0x2
	IFLA_VF_TX_RATE                            = 0x3
	IFLA_VF_SPOOFCHK                           = 0x4
	IFLA_VF_LINK_STATE                         = 0x5
	IFLA_VF_RATE                               = 0x6
	IFLA_VF_RSS_QUERY_EN                       = 0x7
	IFLA_VF_STATS                              = 0x8
	IFLA_VF_TRUST                              = 0x9
	IFLA_VF_IB_NODE_GUID                       = 0xa
	IFLA_VF_IB_PORT_GUID                       = 0xb
	IFLA_VF_VLAN_LIST                          = 0xc
	IFLA_VF_BROADCAST                          = 0xd
	IFLA_VF_VLAN_INFO_UNSPEC                   = 0x0
	IFLA_VF_VLAN_INFO                          = 0x1
	IFLA_VF_LINK_STATE_AUTO                    = 0x0
	IFLA_VF_LINK_STATE_ENABLE                  = 0x1
	IFLA_VF_LINK_STATE_DISABLE                 = 0x2
	IFLA_VF_STATS_RX_PACKETS                   = 0x0
	IFLA_VF_STATS_TX_PACKETS                   = 0x1
	IFLA_VF_STATS_RX_BYTES                     = 0x2
	IFLA_VF_STATS_TX_BYTES                     = 0x3
	IFLA_VF_STATS_BROADCAST                    = 0x4
	IFLA_VF_STATS_MULTICAST                    = 0x5
	IFLA_VF_STATS_PAD                          = 0x6
	IFLA_VF_STATS_RX_DROPPED                   = 0x7
	IFLA_VF_STATS_TX_DROPPED                   = 0x8
	IFLA_VF_PORT_UNSPEC                        = 0x0
	IFLA_VF_PORT                               = 0x1
	IFLA_PORT_UNSPEC                           = 0x0
	IFLA_PORT_VF                               = 0x1
	IFLA_PORT_PROFILE                          = 0x2
	IFLA_PORT_VSI_TYPE                         = 0x3
	IFLA_PORT_INSTANCE_UUID                    = 0x4
	IFLA_PORT_HOST_UUID                        = 0x5
	IFLA_PORT_REQUEST                          = 0x6
	IFLA_PORT_RESPONSE                         = 0x7
	IFLA_IPOIB_UNSPEC                          = 0x0
	IFLA_IPOIB_PKEY                            = 0x1
	IFLA_IPOIB_MODE                            = 0x2
	IFLA_IPOIB_UMCAST                          = 0x3
	IFLA_HSR_UNSPEC                            = 0x0
	IFLA_HSR_SLAVE1                            = 0x1
	IFLA_HSR_SLAVE2                            = 0x2
	IFLA_HSR_MULTICAST_SPEC                    = 0x3
	IFLA_HSR_SUPERVISION_ADDR                  = 0x4
	IFLA_HSR_SEQ_NR                            = 0x5
	IFLA_HSR_VERSION                           = 0x6
	IFLA_HSR_PROTOCOL                          = 0x7
	IFLA_HSR_INTERLINK                         = 0x8
	IFLA_STATS_UNSPEC                          = 0x0
	IFLA_STATS_LINK_64                         = 0x1
	IFLA_STATS_LINK_XSTATS                     = 0x2
	IFLA_STATS_LINK_XSTATS_SLAVE               = 0x3
	IFLA_STATS_LINK_OFFLOAD_XSTATS             = 0x4
	IFLA_STATS_AF_SPEC                         = 0x5
	IFLA_STATS_GETSET_UNSPEC                   = 0x0
	IFLA_STATS_GET_FILTERS                     = 0x1
	IFLA_STATS_SET_OFFLOAD_XSTATS_L3_STATS     = 0x2
	IFLA_OFFLOAD_XSTATS_UNSPEC                 = 0x0
	IFLA_OFFLOAD_XSTATS_CPU_HIT                = 0x1
	IFLA_OFFLOAD_XSTATS_HW_S_INFO              = 0x2
	IFLA_OFFLOAD_XSTATS_L3_STATS               = 0x3
	IFLA_OFFLOAD_XSTATS_HW_S_INFO_UNSPEC       = 0x0
	IFLA_OFFLOAD_XSTATS_HW_S_INFO_REQUEST      = 0x1
	IFLA_OFFLOAD_XSTATS_HW_S_INFO_USED         = 0x2
	IFLA_XDP_UNSPEC                            = 0x0
	IFLA_XDP_FD                                = 0x1
	IFLA_XDP_ATTACHED                          = 0x2
	IFLA_XDP_FLAGS                             = 0x3
	IFLA_XDP_PROG_ID                           = 0x4
	IFLA_XDP_DRV_PROG_ID                       = 0x5
	IFLA_XDP_SKB_PROG_ID                       = 0x6
	IFLA_XDP_HW_PROG_ID                        = 0x7
	IFLA_XDP_EXPECTED_FD                       = 0x8
	IFLA_EVENT_NONE                            = 0x0
	IFLA_EVENT_REBOOT                          = 0x1
	IFLA_EVENT_FEATURES                        = 0x2
	IFLA_EVENT_BONDING_FAILOVER                = 0x3
	IFLA_EVENT_NOTIFY_PEERS                    = 0x4
	IFLA_EVENT_IGMP_RESEND                     = 0x5
	IFLA_EVENT_BONDING_OPTIONS                 = 0x6
	IFLA_TUN_UNSPEC                            = 0x0
	IFLA_TUN_OWNER                             = 0x1
	IFLA_TUN_GROUP                             = 0x2
	IFLA_TUN_TYPE                              = 0x3
	IFLA_TUN_PI                                = 0x4
	IFLA_TUN_VNET_HDR                          = 0x5
	IFLA_TUN_PERSIST                           = 0x6
	IFLA_TUN_MULTI_QUEUE                       = 0x7
	IFLA_TUN_NUM_QUEUES                        = 0x8
	IFLA_TUN_NUM_DISABLED_QUEUES               = 0x9
	IFLA_RMNET_UNSPEC                          = 0x0
	IFLA_RMNET_MUX_ID                          = 0x1
	IFLA_RMNET_FLAGS                           = 0x2
	IFLA_MCTP_UNSPEC                           = 0x0
	IFLA_MCTP_NET                              = 0x1
	IFLA_DSA_UNSPEC                            = 0x0
	IFLA_DSA_CONDUIT                           = 0x1
	IFLA_DSA_MASTER                            = 0x1
)

const (
	NETKIT_NEXT     = -0x1
	NETKIT_PASS     = 0x0
	NETKIT_DROP     = 0x2
	NETKIT_REDIRECT = 0x7
	NETKIT_L2       = 0x0
	NETKIT_L3       = 0x1
)

const (
	NF_INET_PRE_ROUTING  = 0x0
	NF_INET_LOCAL_IN     = 0x1
	NF_INET_FORWARD      = 0x2
	NF_INET_LOCAL_OUT    = 0x3
	NF_INET_POST_ROUTING = 0x4
	NF_INET_NUMHOOKS     = 0x5
)

const (
	NF_NETDEV_INGRESS  = 0x0
	NF_NETDEV_EGRESS   = 0x1
	NF_NETDEV_NUMHOOKS = 0x2
)

const (
	NFPROTO_UNSPEC   = 0x0
	NFPROTO_INET     = 0x1
	NFPROTO_IPV4     = 0x2
	NFPROTO_ARP      = 0x3
	NFPROTO_NETDEV   = 0x5
	NFPROTO_BRIDGE   = 0x7
	NFPROTO_IPV6     = 0xa
	NFPROTO_DECNET   = 0xc
	NFPROTO_NUMPROTO = 0xd
)

const SO_ORIGINAL_DST = 0x50

type Nfgenmsg struct {
	Nfgen_family uint8
	Version      uint8
	Res_id       uint16
}

const (
	NFNL_BATCH_UNSPEC = 0x0
	NFNL_BATCH_GENID  = 0x1
)

const (
	NFT_REG_VERDICT                   = 0x0
	NFT_REG_1                         = 0x1
	NFT_REG_2                         = 0x2
	NFT_REG_3                         = 0x3
	NFT_REG_4                         = 0x4
	NFT_REG32_00                      = 0x8
	NFT_REG32_01                      = 0x9
	NFT_REG32_02                      = 0xa
	NFT_REG32_03                      = 0xb
	NFT_REG32_04                      = 0xc
	NFT_REG32_05                      = 0xd
	NFT_REG32_06                      = 0xe
	NFT_REG32_07                      = 0xf
	NFT_REG32_08                      = 0x10
	NFT_REG32_09                      = 0x11
	NFT_REG32_10                      = 0x12
	NFT_REG32_11                      = 0x13
	NFT_REG32_12                      = 0x14
	NFT_REG32_13                      = 0x15
	NFT_REG32_14                      = 0x16
	NFT_REG32_15                      = 0x17
	NFT_CONTINUE                      = -0x1
	NFT_BREAK                         = -0x2
	NFT_JUMP                          = -0x3
	NFT_GOTO                          = -0x4
	NFT_RETURN                        = -0x5
	NFT_MSG_NEWTABLE                  = 0x0
	NFT_MSG_GETTABLE                  = 0x1
	NFT_MSG_DELTABLE                  = 0x2
	NFT_MSG_NEWCHAIN                  = 0x3
	NFT_MSG_GETCHAIN                  = 0x4
	NFT_MSG_DELCHAIN                  = 0x5
	NFT_MSG_NEWRULE                   = 0x6
	NFT_MSG_GETRULE                   = 0x7
	NFT_MSG_DELRULE                   = 0x8
	NFT_MSG_NEWSET                    = 0x9
	NFT_MSG_GETSET                    = 0xa
	NFT_MSG_DELSET                    = 0xb
	NFT_MSG_NEWSETELEM                = 0xc
	NFT_MSG_GETSETELEM                = 0xd
	NFT_MSG_DELSETELEM                = 0xe
	NFT_MSG_NEWGEN                    = 0xf
	NFT_MSG_GETGEN                    = 0x10
	NFT_MSG_TRACE                     = 0x11
	NFT_MSG_NEWOBJ                    = 0x12
	NFT_MSG_GETOBJ                    = 0x13
	NFT_MSG_DELOBJ                    = 0x14
	NFT_MSG_GETOBJ_RESET              = 0x15
	NFT_MSG_NEWFLOWTABLE              = 0x16
	NFT_MSG_GETFLOWTABLE              = 0x17
	NFT_MSG_DELFLOWTABLE              = 0x18
	NFT_MSG_GETRULE_RESET             = 0x19
	NFT_MSG_MAX                       = 0x22
	NFTA_LIST_UNSPEC                  = 0x0
	NFTA_LIST_ELEM                    = 0x1
	NFTA_HOOK_UNSPEC                  = 0x0
	NFTA_HOOK_HOOKNUM                 = 0x1
	NFTA_HOOK_PRIORITY                = 0x2
	NFTA_HOOK_DEV                     = 0x3
	NFT_TABLE_F_DORMANT               = 0x1
	NFTA_TABLE_UNSPEC                 = 0x0
	NFTA_TABLE_NAME                   = 0x1
	NFTA_TABLE_FLAGS                  = 0x2
	NFTA_TABLE_USE                    = 0x3
	NFTA_CHAIN_UNSPEC                 = 0x0
	NFTA_CHAIN_TABLE                  = 0x1
	NFTA_CHAIN_HANDLE                 = 0x2
	NFTA_CHAIN_NAME                   = 0x3
	NFTA_CHAIN_HOOK                   = 0x4
	NFTA_CHAIN_POLICY                 = 0x5
	NFTA_CHAIN_USE                    = 0x6
	NFTA_CHAIN_TYPE                   = 0x7
	NFTA_CHAIN_COUNTERS               = 0x8
	NFTA_CHAIN_PAD                    = 0x9
	NFTA_RULE_UNSPEC                  = 0x0
	NFTA_RULE_TABLE                   = 0x1
	NFTA_RULE_CHAIN                   = 0x2
	NFTA_RULE_HANDLE                  = 0x3
	NFTA_RULE_EXPRESSIONS             = 0x4
	NFTA_RULE_COMPAT                  = 0x5
	NFTA_RULE_POSITION                = 0x6
	NFTA_RULE_USERDATA                = 0x7
	NFTA_RULE_PAD                     = 0x8
	NFTA_RULE_ID                      = 0x9
	NFT_RULE_COMPAT_F_INV             = 0x2
	NFT_RULE_COMPAT_F_MASK            = 0x2
	NFTA_RULE_COMPAT_UNSPEC           = 0x0
	NFTA_RULE_COMPAT_PROTO            = 0x1
	NFTA_RULE_COMPAT_FLAGS            = 0x2
	NFT_SET_ANONYMOUS                 = 0x1
	NFT_SET_CONSTANT                  = 0x2
	NFT_SET_INTERVAL                  = 0x4
	NFT_SET_MAP                       = 0x8
	NFT_SET_TIMEOUT                   = 0x10
	NFT_SET_EVAL                      = 0x20
	NFT_SET_OBJECT                    = 0x40
	NFT_SET_POL_PERFORMANCE           = 0x0
	NFT_SET_POL_MEMORY                = 0x1
	NFTA_SET_DESC_UNSPEC              = 0x0
	NFTA_SET_DESC_SIZE                = 0x1
	NFTA_SET_UNSPEC                   = 0x0
	NFTA_SET_TABLE                    = 0x1
	NFTA_SET_NAME                     = 0x2
	NFTA_SET_FLAGS                    = 0x3
	NFTA_SET_KEY_TYPE                 = 0x4
	NFTA_SET_KEY_LEN                  = 0x5
	NFTA_SET_DATA_TYPE                = 0x6
	NFTA_SET_DATA_LEN                 = 0x7
	NFTA_SET_POLICY                   = 0x8
	NFTA_SET_DESC                     = 0x9
	NFTA_SET_ID                       = 0xa
	NFTA_SET_TIMEOUT                  = 0xb
	NFTA_SET_GC_INTERVAL              = 0xc
	NFTA_SET_USERDATA                 = 0xd
	NFTA_SET_PAD                      = 0xe
	NFTA_SET_OBJ_TYPE                 = 0xf
	NFT_SET_ELEM_INTERVAL_END         = 0x1
	NFTA_SET_ELEM_UNSPEC              = 0x0
	NFTA_SET_ELEM_KEY                 = 0x1
	NFTA_SET_ELEM_DATA                = 0x2
	NFTA_SET_ELEM_FLAGS               = 0x3
	NFTA_SET_ELEM_TIMEOUT             = 0x4
	NFTA_SET_ELEM_EXPIRATION          = 0x5
	NFTA_SET_ELEM_USERDATA            = 0x6
	NFTA_SET_ELEM_EXPR                = 0x7
	NFTA_SET_ELEM_PAD                 = 0x8
	NFTA_SET_ELEM_OBJREF              = 0x9
	NFTA_SET_ELEM_LIST_UNSPEC         = 0x0
	NFTA_SET_ELEM_LIST_TABLE          = 0x1
	NFTA_SET_ELEM_LIST_SET            = 0x2
	NFTA_SET_ELEM_LIST_ELEMENTS       = 0x3
	NFTA_SET_ELEM_LIST_SET_ID         = 0x4
	NFT_DATA_VALUE                    = 0x0
	NFT_DATA_VERDICT                  = 0xffffff00
	NFTA_DATA_UNSPEC                  = 0x0
	NFTA_DATA_VALUE                   = 0x1
	NFTA_DATA_VERDICT                 = 0x2
	NFTA_VERDICT_UNSPEC               = 0x0
	NFTA_VERDICT_CODE                 = 0x1
	NFTA_VERDICT_CHAIN                = 0x2
	NFTA_EXPR_UNSPEC                  = 0x0
	NFTA_EXPR_NAME                    = 0x1
	NFTA_EXPR_DATA                    = 0x2
	NFTA_IMMEDIATE_UNSPEC             = 0x0
	NFTA_IMMEDIATE_DREG               = 0x1
	NFTA_IMMEDIATE_DATA               = 0x2
	NFTA_BITWISE_UNSPEC               = 0x0
	NFTA_BITWISE_SREG                 = 0x1
	NFTA_BITWISE_DREG                 = 0x2
	NFTA_BITWISE_LEN                  = 0x3
	NFTA_BITWISE_MASK                 = 0x4
	NFTA_BITWISE_XOR                  = 0x5
	NFT_BYTEORDER_NTOH                = 0x0
	NFT_BYTEORDER_HTON                = 0x1
	NFTA_BYTEORDER_UNSPEC             = 0x0
	NFTA_BYTEORDER_SREG               = 0x1
	NFTA_BYTEORDER_DREG               = 0x2
	NFTA_BYTEORDER_OP                 = 0x3
	NFTA_BYTEORDER_LEN                = 0x4
	NFTA_BYTEORDER_SIZE               = 0x5
	NFT_CMP_EQ                        = 0x0
	NFT_CMP_NEQ                       = 0x1
	NFT_CMP_LT                        = 0x2
	NFT_CMP_LTE                       = 0x3
	NFT_CMP_GT                        = 0x4
	NFT_CMP_GTE                       = 0x5
	NFTA_CMP_UNSPEC                   = 0x0
	NFTA_CMP_SREG                     = 0x1
	NFTA_CMP_OP                       = 0x2
	NFTA_CMP_DATA                     = 0x3
	NFT_RANGE_EQ                      = 0x0
	NFT_RANGE_NEQ                     = 0x1
	NFTA_RANGE_UNSPEC                 = 0x0
	NFTA_RANGE_SREG                   = 0x1
	NFTA_RANGE_OP                     = 0x2
	NFTA_RANGE_FROM_DATA              = 0x3
	NFTA_RANGE_TO_DATA                = 0x4
	NFT_LOOKUP_F_INV                  = 0x1
	NFTA_LOOKUP_UNSPEC                = 0x0
	NFTA_LOOKUP_SET                   = 0x1
	NFTA_LOOKUP_SREG                  = 0x2
	NFTA_LOOKUP_DREG                  = 0x3
	NFTA_LOOKUP_SET_ID                = 0x4
	NFTA_LOOKUP_FLAGS                 = 0x5
	NFT_DYNSET_OP_ADD                 = 0x0
	NFT_DYNSET_OP_UPDATE              = 0x1
	NFT_DYNSET_F_INV                  = 0x1
	NFTA_DYNSET_UNSPEC                = 0x0
	NFTA_DYNSET_SET_NAME              = 0x1
	NFTA_DYNSET_SET_ID                = 0x2
	NFTA_DYNSET_OP                    = 0x3
	NFTA_DYNSET_SREG_KEY              = 0x4
	NFTA_DYNSET_SREG_DATA             = 0x5
	NFTA_DYNSET_TIMEOUT               = 0x6
	NFTA_DYNSET_EXPR                  = 0x7
	NFTA_DYNSET_PAD                   = 0x8
	NFTA_DYNSET_FLAGS                 = 0x9
	NFT_PAYLOAD_LL_HEADER             = 0x0
	NFT_PAYLOAD_NETWORK_HEADER        = 0x1
	NFT_PAYLOAD_TRANSPORT_HEADER      = 0x2
	NFT_PAYLOAD_INNER_HEADER          = 0x3
	NFT_PAYLOAD_TUN_HEADER            = 0x4
	NFT_PAYLOAD_CSUM_NONE             = 0x0
	NFT_PAYLOAD_CSUM_INET             = 0x1
	NFT_PAYLOAD_CSUM_SCTP             = 0x2
	NFT_PAYLOAD_L4CSUM_PSEUDOHDR      = 0x1
	NFTA_PAYLOAD_UNSPEC               = 0x0
	NFTA_PAYLOAD_DREG                 = 0x1
	NFTA_PAYLOAD_BASE                 = 0x2
	NFTA_PAYLOAD_OFFSET               = 0x3
	NFTA_PAYLOAD_LEN                  = 0x4
	NFTA_PAYLOAD_SREG                 = 0x5
	NFTA_PAYLOAD_CSUM_TYPE            = 0x6
	NFTA_PAYLOAD_CSUM_OFFSET          = 0x7
	NFTA_PAYLOAD_CSUM_FLAGS           = 0x8
	NFT_EXTHDR_F_PRESENT              = 0x1
	NFT_EXTHDR_OP_IPV6                = 0x0
	NFT_EXTHDR_OP_TCPOPT              = 0x1
	NFTA_EXTHDR_UNSPEC                = 0x0
	NFTA_EXTHDR_DREG                  = 0x1
	NFTA_EXTHDR_TYPE                  = 0x2
	NFTA_EXTHDR_OFFSET                = 0x3
	NFTA_EXTHDR_LEN                   = 0x4
	NFTA_EXTHDR_FLAGS                 = 0x5
	NFTA_EXTHDR_OP                    = 0x6
	NFTA_EXTHDR_SREG                  = 0x7
	NFT_META_LEN                      = 0x0
	NFT_META_PROTOCOL                 = 0x1
	NFT_META_PRIORITY                 = 0x2
	NFT_META_MARK                     = 0x3
	NFT_META_IIF                      = 0x4
	NFT_META_OIF                      = 0x5
	NFT_META_IIFNAME                  = 0x6
	NFT_META_OIFNAME                  = 0x7
	NFT_META_IIFTYPE                  = 0x8
	NFT_META_OIFTYPE                  = 0x9
	NFT_META_SKUID                    = 0xa
	NFT_META_SKGID                    = 0xb
	NFT_META_NFTRACE                  = 0xc
	NFT_META_RTCLASSID                = 0xd
	NFT_META_SECMARK                  = 0xe
	NFT_META_NFPROTO                  = 0xf
	NFT_META_L4PROTO                  = 0x10
	NFT_META_BRI_IIFNAME              = 0x11
	NFT_META_BRI_OIFNAME              = 0x12
	NFT_META_PKTTYPE                  = 0x13
	NFT_META_CPU                      = 0x14
	NFT_META_IIFGROUP                 = 0x15
	NFT_META_OIFGROUP                 = 0x16
	NFT_META_CGROUP                   = 0x17
	NFT_META_PRANDOM                  = 0x18
	NFT_RT_CLASSID                    = 0x0
	NFT_RT_NEXTHOP4                   = 0x1
	NFT_RT_NEXTHOP6                   = 0x2
	NFT_RT_TCPMSS                     = 0x3
	NFT_HASH_JENKINS                  = 0x0
	NFT_HASH_SYM                      = 0x1
	NFTA_HASH_UNSPEC                  = 0x0
	NFTA_HASH_SREG                    = 0x1
	NFTA_HASH_DREG                    = 0x2
	NFTA_HASH_LEN                     = 0x3
	NFTA_HASH_MODULUS                 = 0x4
	NFTA_HASH_SEED                    = 0x5
	NFTA_HASH_OFFSET                  = 0x6
	NFTA_HASH_TYPE                    = 0x7
	NFTA_META_UNSPEC                  = 0x0
	NFTA_META_DREG                    = 0x1
	NFTA_META_KEY                     = 0x2
	NFTA_META_SREG                    = 0x3
	NFTA_RT_UNSPEC                    = 0x0
	NFTA_RT_DREG                      = 0x1
	NFTA_RT_KEY                       = 0x2
	NFT_CT_STATE                      = 0x0
	NFT_CT_DIRECTION                  = 0x1
	NFT_CT_STATUS                     = 0x2
	NFT_CT_MARK                       = 0x3
	NFT_CT_SECMARK                    = 0x4
	NFT_CT_EXPIRATION                 = 0x5
	NFT_CT_HELPER                     = 0x6
	NFT_CT_L3PROTOCOL                 = 0x7
	NFT_CT_SRC                        = 0x8
	NFT_CT_DST                        = 0x9
	NFT_CT_PROTOCOL                   = 0xa
	NFT_CT_PROTO_SRC                  = 0xb
	NFT_CT_PROTO_DST                  = 0xc
	NFT_CT_LABELS                     = 0xd
	NFT_CT_PKTS                       = 0xe
	NFT_CT_BYTES                      = 0xf
	NFT_CT_AVGPKT                     = 0x10
	NFT_CT_ZONE                       = 0x11
	NFT_CT_EVENTMASK                  = 0x12
	NFT_CT_SRC_IP                     = 0x13
	NFT_CT_DST_IP                     = 0x14
	NFT_CT_SRC_IP6                    = 0x15
	NFT_CT_DST_IP6                    = 0x16
	NFT_CT_ID                         = 0x17
	NFTA_CT_UNSPEC                    = 0x0
	NFTA_CT_DREG                      = 0x1
	NFTA_CT_KEY                       = 0x2
	NFTA_CT_DIRECTION                 = 0x3
	NFTA_CT_SREG                      = 0x4
	NFT_LIMIT_PKTS                    = 0x0
	NFT_LIMIT_PKT_BYTES               = 0x1
	NFT_LIMIT_F_INV                   = 0x1
	NFTA_LIMIT_UNSPEC                 = 0x0
	NFTA_LIMIT_RATE                   = 0x1
	NFTA_LIMIT_UNIT                   = 0x2
	NFTA_LIMIT_BURST                  = 0x3
	NFTA_LIMIT_TYPE                   = 0x4
	NFTA_LIMIT_FLAGS                  = 0x5
	NFTA_LIMIT_PAD                    = 0x6
	NFTA_COUNTER_UNSPEC               = 0x0
	NFTA_COUNTER_BYTES                = 0x1
	NFTA_COUNTER_PACKETS              = 0x2
	NFTA_COUNTER_PAD                  = 0x3
	NFTA_LOG_UNSPEC                   = 0x0
	NFTA_LOG_GROUP                    = 0x1
	NFTA_LOG_PREFIX                   = 0x2
	NFTA_LOG_SNAPLEN                  = 0x3
	NFTA_LOG_QTHRESHOLD               = 0x4
	NFTA_LOG_LEVEL                    = 0x5
	NFTA_LOG_FLAGS                    = 0x6
	NFTA_QUEUE_UNSPEC                 = 0x0
	NFTA_QUEUE_NUM                    = 0x1
	NFTA_QUEUE_TOTAL                  = 0x2
	NFTA_QUEUE_FLAGS                  = 0x3
	NFTA_QUEUE_SREG_QNUM              = 0x4
	NFT_QUOTA_F_INV                   = 0x1
	NFT_QUOTA_F_DEPLETED              = 0x2
	NFTA_QUOTA_UNSPEC                 = 0x0
	NFTA_QUOTA_BYTES                  = 0x1
	NFTA_QUOTA_FLAGS                  = 0x2
	NFTA_QUOTA_PAD                    = 0x3
	NFTA_QUOTA_CONSUMED               = 0x4
	NFT_REJECT_ICMP_UNREACH           = 0x0
	NFT_REJECT_TCP_RST                = 0x1
	NFT_REJECT_ICMPX_UNREACH          = 0x2
	NFT_REJECT_ICMPX_NO_ROUTE         = 0x0
	NFT_REJECT_ICMPX_PORT_UNREACH     = 0x1
	NFT_REJECT_ICMPX_HOST_UNREACH     = 0x2
	NFT_REJECT_ICMPX_ADMIN_PROHIBITED = 0x3
	NFTA_REJECT_UNSPEC                = 0x0
	NFTA_REJECT_TYPE                  = 0x1
	NFTA_REJECT_ICMP_CODE             = 0x2
	NFT_NAT_SNAT                      = 0x0
	NFT_NAT_DNAT                      = 0x1
	NFTA_NAT_UNSPEC                   = 0x0
	NFTA_NAT_TYPE                     = 0x1
	NFTA_NAT_FAMILY                   = 0x2
	NFTA_NAT_REG_ADDR_MIN             = 0x3
	NFTA_NAT_REG_ADDR_MAX             = 0x4
	NFTA_NAT_REG_PROTO_MIN            = 0x5
	NFTA_NAT_REG_PROTO_MAX            = 0x6
	NFTA_NAT_FLAGS                    = 0x7
	NFTA_MASQ_UNSPEC                  = 0x0
	NFTA_MASQ_FLAGS                   = 0x1
	NFTA_MASQ_REG_PROTO_MIN           = 0x2
	NFTA_MASQ_REG_PROTO_MAX           = 0x3
	NFTA_REDIR_UNSPEC                 = 0x0
	NFTA_REDIR_REG_PROTO_MIN          = 0x1
	NFTA_REDIR_REG_PROTO_MAX          = 0x2
	NFTA_REDIR_FLAGS                  = 0x3
	NFTA_DUP_UNSPEC                   = 0x0
	NFTA_DUP_SREG_ADDR                = 0x1
	NFTA_DUP_SREG_DEV                 = 0x2
	NFTA_FWD_UNSPEC                   = 0x0
	NFTA_FWD_SREG_DEV                 = 0x1
	NFTA_OBJREF_UNSPEC                = 0x0
	NFTA_OBJREF_IMM_TYPE              = 0x1
	NFTA_OBJREF_IMM_NAME              = 0x2
	NFTA_OBJREF_SET_SREG              = 0x3
	NFTA_OBJREF_SET_NAME              = 0x4
	NFTA_OBJREF_SET_ID                = 0x5
	NFTA_GEN_UNSPEC                   = 0x0
	NFTA_GEN_ID                       = 0x1
	NFTA_GEN_PROC_PID                 = 0x2
	NFTA_GEN_PROC_NAME                = 0x3
	NFTA_FIB_UNSPEC                   = 0x0
	NFTA_FIB_DREG                     = 0x1
	NFTA_FIB_RESULT                   = 0x2
	NFTA_FIB_FLAGS                    = 0x3
	NFT_FIB_RESULT_UNSPEC             = 0x0
	NFT_FIB_RESULT_OIF                = 0x1
	NFT_FIB_RESULT_OIFNAME            = 0x2
	NFT_FIB_RESULT_ADDRTYPE           = 0x3
	NFTA_FIB_F_SADDR                  = 0x1
	NFTA_FIB_F_DADDR                  = 0x2
	NFTA_FIB_F_MARK                   = 0x4
	NFTA_FIB_F_IIF                    = 0x8
	NFTA_FIB_F_OIF                    = 0x10
	NFTA_FIB_F_PRESENT                = 0x20
	NFTA_CT_HELPER_UNSPEC             = 0x0
	NFTA_CT_HELPER_NAME               = 0x1
	NFTA_CT_HELPER_L3PROTO            = 0x2
	NFTA_CT_HELPER_L4PROTO            = 0x3
	NFTA_OBJ_UNSPEC                   = 0x0
	NFTA_OBJ_TABLE                    = 0x1
	NFTA_OBJ_NAME                     = 0x2
	NFTA_OBJ_TYPE                     = 0x3
	NFTA_OBJ_DATA                     = 0x4
	NFTA_OBJ_USE                      = 0x5
	NFTA_TRACE_UNSPEC                 = 0x0
	NFTA_TRACE_TABLE                  = 0x1
	NFTA_TRACE_CHAIN                  = 0x2
	NFTA_TRACE_RULE_HANDLE            = 0x3
	NFTA_TRACE_TYPE                   = 0x4
	NFTA_TRACE_VERDICT                = 0x5
	NFTA_TRACE_ID                     = 0x6
	NFTA_TRACE_LL_HEADER              = 0x7
	NFTA_TRACE_NETWORK_HEADER         = 0x8
	NFTA_TRACE_TRANSPORT_HEADER       = 0x9
	NFTA_TRACE_IIF                    = 0xa
	NFTA_TRACE_IIFTYPE                = 0xb
	NFTA_TRACE_OIF                    = 0xc
	NFTA_TRACE_OIFTYPE                = 0xd
	NFTA_TRACE_MARK                   = 0xe
	NFTA_TRACE_NFPROTO                = 0xf
	NFTA_TRACE_POLICY                 = 0x10
	NFTA_TRACE_PAD                    = 0x11
	NFT_TRACETYPE_UNSPEC              = 0x0
	NFT_TRACETYPE_POLICY              = 0x1
	NFT_TRACETYPE_RETURN              = 0x2
	NFT_TRACETYPE_RULE                = 0x3
	NFTA_NG_UNSPEC                    = 0x0
	NFTA_NG_DREG                      = 0x1
	NFTA_NG_MODULUS                   = 0x2
	NFTA_NG_TYPE                      = 0x3
	NFTA_NG_OFFSET                    = 0x4
	NFT_NG_INCREMENTAL                = 0x0
	NFT_NG_RANDOM                     = 0x1
)

const (
	NFTA_TARGET_UNSPEC = 0x0
	NFTA_TARGET_NAME   = 0x1
	NFTA_TARGET_REV    = 0x2
	NFTA_TARGET_INFO   = 0x3
	NFTA_MATCH_UNSPEC  = 0x0
	NFTA_MATCH_NAME    = 0x1
	NFTA_MATCH_REV     = 0x2
	NFTA_MATCH_INFO    = 0x3
	NFTA_COMPAT_UNSPEC = 0x0
	NFTA_COMPAT_NAME   = 0x1
	NFTA_COMPAT_REV    = 0x2
	NFTA_COMPAT_TYPE   = 0x3
)

type RTCTime struct {
	Sec   int32
	Min   int32
	Hour  int32
	Mday  int32
	Mon   int32
	Year  int32
	Wday  int32
	Yday  int32
	Isdst int32
}

type RTCWkAlrm struct {
	Enabled uint8
	Pending uint8
	Time    RTCTime
}

type BlkpgIoctlArg struct {
	Op      int32
	Flags   int32
	Datalen int32
	Data    *byte
}

const (
	BLKPG_ADD_PARTITION    = 0x1
	BLKPG_DEL_PARTITION    = 0x2
	BLKPG_RESIZE_PARTITION = 0x3
)

const (
	NETNSA_NONE         = 0x0
	NETNSA_NSID         = 0x1
	NETNSA_PID          = 0x2
	NETNSA_FD           = 0x3
	NETNSA_TARGET_NSID  = 0x4
	NETNSA_CURRENT_NSID = 0x5
)

type XDPRingOffset struct {
	Producer uint64
	Consumer uint64
	Desc     uint64
	Flags    uint64
}

type XDPMmapOffsets struct {
	Rx XDPRingOffset
	Tx XDPRingOffset
	Fr XDPRingOffset
	Cr XDPRingOffset
}

type XDPUmemReg struct {
	Addr            uint64
	Len             uint64
	Size            uint32
	Headroom        uint32
	Flags           uint32
	Tx_metadata_len uint32
}

type XDPStatistics struct {
	Rx_dropped               uint64
	Rx_invalid_descs         uint64
	Tx_invalid_descs         uint64
	Rx_ring_full             uint64
	Rx_fill_ring_empty_descs uint64
	Tx_ring_empty_descs      uint64
}

type XDPDesc struct {
	Addr    uint64
	Len     uint32
	Options uint32
}

const (
	NCSI_CMD_UNSPEC                 = 0x0
	NCSI_CMD_PKG_INFO               = 0x1
	NCSI_CMD_SET_INTERFACE          = 0x2
	NCSI_CMD_CLEAR_INTERFACE        = 0x3
	NCSI_ATTR_UNSPEC                = 0x0
	NCSI_ATTR_IFINDEX               = 0x1
	NCSI_ATTR_PACKAGE_LIST          = 0x2
	NCSI_ATTR_PACKAGE_ID            = 0x3
	NCSI_ATTR_CHANNEL_ID            = 0x4
	NCSI_PKG_ATTR_UNSPEC            = 0x0
	NCSI_PKG_ATTR                   = 0x1
	NCSI_PKG_ATTR_ID                = 0x2
	NCSI_PKG_ATTR_FORCED            = 0x3
	NCSI_PKG_ATTR_CHANNEL_LIST      = 0x4
	NCSI_CHANNEL_ATTR_UNSPEC        = 0x0
	NCSI_CHANNEL_ATTR               = 0x1
	NCSI_CHANNEL_ATTR_ID            = 0x2
	NCSI_CHANNEL_ATTR_VERSION_MAJOR = 0x3
	NCSI_CHANNEL_ATTR_VERSION_MINOR = 0x4
	NCSI_CHANNEL_ATTR_VERSION_STR   = 0x5
	NCSI_CHANNEL_ATTR_LINK_STATE    = 0x6
	NCSI_CHANNEL_ATTR_ACTIVE        = 0x7
	NCSI_CHANNEL_ATTR_FORCED        = 0x8
	NCSI_CHANNEL_ATTR_VLAN_LIST     = 0x9
	NCSI_CHANNEL_ATTR_VLAN_ID       = 0xa
)

type ScmTimestamping struct {
	Ts [3]Timespec
}

const (
	SOF_TIMESTAMPING_TX_HARDWARE  = 0x1
	SOF_TIMESTAMPING_TX_SOFTWARE  = 0x2
	SOF_TIMESTAMPING_RX_HARDWARE  = 0x4
	SOF_TIMESTAMPING_RX_SOFTWARE  = 0x8
	SOF_TIMESTAMPING_SOFTWARE     = 0x10
	SOF_TIMESTAMPING_SYS_HARDWARE = 0x20
	SOF_TIMESTAMPING_RAW_HARDWARE = 0x40
	SOF_TIMESTAMPING_OPT_ID       = 0x80
	SOF_TIMESTAMPING_TX_SCHED     = 0x100
	SOF_TIMESTAMPING_TX_ACK       = 0x200
	SOF_TIMESTAMPING_OPT_CMSG     = 0x400
	SOF_TIMESTAMPING_OPT_TSONLY   = 0x800
	SOF_TIMESTAMPING_OPT_STATS    = 0x1000
	SOF_TIMESTAMPING_OPT_PKTINFO  = 0x2000
	SOF_TIMESTAMPING_OPT_TX_SWHW  = 0x4000
	SOF_TIMESTAMPING_BIND_PHC     = 0x8000
	SOF_TIMESTAMPING_OPT_ID_TCP   = 0x10000

	SOF_TIMESTAMPING_LAST = 0x40000
	SOF_TIMESTAMPING_MASK = 0x7ffff

	SCM_TSTAMP_SND   = 0x0
	SCM_TSTAMP_SCHED = 0x1
	SCM_TSTAMP_ACK   = 0x2
)

type SockExtendedErr struct {
	Errno  uint32
	Origin uint8
	Type   uint8
	Code   uint8
	Pad    uint8
	Info   uint32
	Data   uint32
}

type FanotifyEventMetadata struct {
	Event_len    uint32
	Vers         uint8
	Reserved     uint8
	Metadata_len uint16
	Mask         uint64
	Fd           int32
	Pid          int32
}

type FanotifyResponse struct {
	Fd       int32
	Response uint32
}

const (
	CRYPTO_MSG_BASE      = 0x10
	CRYPTO_MSG_NEWALG    = 0x10
	CRYPTO_MSG_DELALG    = 0x11
	CRYPTO_MSG_UPDATEALG = 0x12
	CRYPTO_MSG_GETALG    = 0x13
	CRYPTO_MSG_DELRNG    = 0x14
	CRYPTO_MSG_GETSTAT   = 0x15
)

const (
	CRYPTOCFGA_UNSPEC           = 0x0
	CRYPTOCFGA_PRIORITY_VAL     = 0x1
	CRYPTOCFGA_REPORT_LARVAL    = 0x2
	CRYPTOCFGA_REPORT_HASH      = 0x3
	CRYPTOCFGA_REPORT_BLKCIPHER = 0x4
	CRYPTOCFGA_REPORT_AEAD      = 0x5
	CRYPTOCFGA_REPORT_COMPRESS  = 0x6
	CRYPTOCFGA_REPORT_RNG       = 0x7
	CRYPTOCFGA_REPORT_CIPHER    = 0x8
	CRYPTOCFGA_REPORT_AKCIPHER  = 0x9
	CRYPTOCFGA_REPORT_KPP       = 0xa
	CRYPTOCFGA_REPORT_ACOMP     = 0xb
	CRYPTOCFGA_STAT_LARVAL      = 0xc
	CRYPTOCFGA_STAT_HASH        = 0xd
	CRYPTOCFGA_STAT_BLKCIPHER   = 0xe
	CRYPTOCFGA_STAT_AEAD        = 0xf
	CRYPTOCFGA_STAT_COMPRESS    = 0x10
	CRYPTOCFGA_STAT_RNG         = 0x11
	CRYPTOCFGA_STAT_CIPHER      = 0x12
	CRYPTOCFGA_STAT_AKCIPHER    = 0x13
	CRYPTOCFGA_STAT_KPP         = 0x14
	CRYPTOCFGA_STAT_ACOMP       = 0x15
)

const (
	BPF_REG_0                                  = 0x0
	BPF_REG_1                                  = 0x1
	BPF_REG_2                                  = 0x2
	BPF_REG_3                                  = 0x3
	BPF_REG_4                                  = 0x4
	BPF_REG_5                                  = 0x5
	BPF_REG_6                                  = 0x6
	BPF_REG_7                                  = 0x7
	BPF_REG_8                                  = 0x8
	BPF_REG_9                                  = 0x9
	BPF_REG_10                                 = 0xa
	BPF_CGROUP_ITER_ORDER_UNSPEC               = 0x0
	BPF_CGROUP_ITER_SELF_ONLY                  = 0x1
	BPF_CGROUP_ITER_DESCENDANTS_PRE            = 0x2
	BPF_CGROUP_ITER_DESCENDANTS_POST           = 0x3
	BPF_CGROUP_ITER_ANCESTORS_UP               = 0x4
	BPF_MAP_CREATE                             = 0x0
	BPF_MAP_LOOKUP_ELEM                        = 0x1
	BPF_MAP_UPDATE_ELEM                        = 0x2
	BPF_MAP_DELETE_ELEM                        = 0x3
	BPF_MAP_GET_NEXT_KEY                       = 0x4
	BPF_PROG_LOAD                              = 0x5
	BPF_OBJ_PIN                                = 0x6
	BPF_OBJ_GET                                = 0x7
	BPF_PROG_ATTACH                            = 0x8
	BPF_PROG_DETACH                            = 0x9
	BPF_PROG_TEST_RUN                          = 0xa
	BPF_PROG_RUN                               = 0xa
	BPF_PROG_GET_NEXT_ID                       = 0xb
	BPF_MAP_GET_NEXT_ID                        = 0xc
	BPF_PROG_GET_FD_BY_ID                      = 0xd
	BPF_MAP_GET_FD_BY_ID                       = 0xe
	BPF_OBJ_GET_INFO_BY_FD                     = 0xf
	BPF_PROG_QUERY                             = 0x10
	BPF_RAW_TRACEPOINT_OPEN                    = 0x11
	BPF_BTF_LOAD                               = 0x12
	BPF_BTF_GET_FD_BY_ID                       = 0x13
	BPF_TASK_FD_QUERY                          = 0x14
	BPF_MAP_LOOKUP_AND_DELETE_ELEM             = 0x15
	BPF_MAP_FREEZE                             = 0x16
	BPF_BTF_GET_NEXT_ID                        = 0x17
	BPF_MAP_LOOKUP_BATCH                       = 0x18
	BPF_MAP_LOOKUP_AND_DELETE_BATCH            = 0x19
	BPF_MAP_UPDATE_BATCH                       = 0x1a
	BPF_MAP_DELETE_BATCH                       = 0x1b
	BPF_LINK_CREATE                            = 0x1c
	BPF_LINK_UPDATE                            = 0x1d
	BPF_LINK_GET_FD_BY_ID                      = 0x1e
	BPF_LINK_GET_NEXT_ID                       = 0x1f
	BPF_ENABLE_STATS                           = 0x20
	BPF_ITER_CREATE                            = 0x21
	BPF_LINK_DETACH                            = 0x22
	BPF_PROG_BIND_MAP                          = 0x23
	BPF_MAP_TYPE_UNSPEC                        = 0x0
	BPF_MAP_TYPE_HASH                          = 0x1
	BPF_MAP_TYPE_ARRAY                         = 0x2
	BPF_MAP_TYPE_PROG_ARRAY                    = 0x3
	BPF_MAP_TYPE_PERF_EVENT_ARRAY              = 0x4
	BPF_MAP_TYPE_PERCPU_HASH                   = 0x5
	BPF_MAP_TYPE_PERCPU_ARRAY                  = 0x6
	BPF_MAP_TYPE_STACK_TRACE                   = 0x7
	BPF_MAP_TYPE_CGROUP_ARRAY                  = 0x8
	BPF_MAP_TYPE_LRU_HASH                      = 0x9
	BPF_MAP_TYPE_LRU_PERCPU_HASH               = 0xa
	BPF_MAP_TYPE_LPM_TRIE                      = 0xb
	BPF_MAP_TYPE_ARRAY_OF_MAPS                 = 0xc
	BPF_MAP_TYPE_HASH_OF_MAPS                  = 0xd
	BPF_MAP_TYPE_DEVMAP                        = 0xe
	BPF_MAP_TYPE_SOCKMAP                       = 0xf
	BPF_MAP_TYPE_CPUMAP                        = 0x10
	BPF_MAP_TYPE_XSKMAP                        = 0x11
	BPF_MAP_TYPE_SOCKHASH                      = 0x12
	BPF_MAP_TYPE_CGROUP_STORAGE_DEPRECATED     = 0x13
	BPF_MAP_TYPE_CGROUP_STORAGE                = 0x13
	BPF_MAP_TYPE_REUSEPORT_SOCKARRAY           = 0x14
	BPF_MAP_TYPE_PERCPU_CGROUP_STORAGE         = 0x15
	BPF_MAP_TYPE_QUEUE                         = 0x16
	BPF_MAP_TYPE_STACK                         = 0x17
	BPF_MAP_TYPE_SK_STORAGE                    = 0x18
	BPF_MAP_TYPE_DEVMAP_HASH                   = 0x19
	BPF_MAP_TYPE_STRUCT_OPS                    = 0x1a
	BPF_MAP_TYPE_RINGBUF                       = 0x1b
	BPF_MAP_TYPE_INODE_STORAGE                 = 0x1c
	BPF_MAP_TYPE_TASK_STORAGE                  = 0x1d
	BPF_MAP_TYPE_BLOOM_FILTER                  = 0x1e
	BPF_MAP_TYPE_USER_RINGBUF                  = 0x1f
	BPF_MAP_TYPE_CGRP_STORAGE                  = 0x20
	BPF_PROG_TYPE_UNSPEC                       = 0x0
	BPF_PROG_TYPE_SOCKET_FILTER                = 0x1
	BPF_PROG_TYPE_KPROBE                       = 0x2
	BPF_PROG_TYPE_SCHED_CLS                    = 0x3
	BPF_PROG_TYPE_SCHED_ACT                    = 0x4
	BPF_PROG_TYPE_TRACEPOINT                   = 0x5
	BPF_PROG_TYPE_XDP                          = 0x6
	BPF_PROG_TYPE_PERF_EVENT                   = 0x7
	BPF_PROG_TYPE_CGROUP_SKB                   = 0x8
	BPF_PROG_TYPE_CGROUP_SOCK                  = 0x9
	BPF_PROG_TYPE_LWT_IN                       = 0xa
	BPF_PROG_TYPE_LWT_OUT                      = 0xb
	BPF_PROG_TYPE_LWT_XMIT                     = 0xc
	BPF_PROG_TYPE_SOCK_OPS                     = 0xd
	BPF_PROG_TYPE_SK_SKB                       = 0xe
	BPF_PROG_TYPE_CGROUP_DEVICE                = 0xf
	BPF_PROG_TYPE_SK_MSG                       = 0x10
	BPF_PROG_TYPE_RAW_TRACEPOINT               = 0x11
	BPF_PROG_TYPE_CGROUP_SOCK_ADDR             = 0x12
	BPF_PROG_TYPE_LWT_SEG6LOCAL                = 0x13
	BPF_PROG_TYPE_LIRC_MODE2                   = 0x14
	BPF_PROG_TYPE_SK_REUSEPORT                 = 0x15
	BPF_PROG_TYPE_FLOW_DISSECTOR               = 0x16
	BPF_PROG_TYPE_CGROUP_SYSCTL                = 0x17
	BPF_PROG_TYPE_RAW_TRACEPOINT_WRITABLE      = 0x18
	BPF_PROG_TYPE_CGROUP_SOCKOPT               = 0x19
	BPF_PROG_TYPE_TRACING                      = 0x1a
	BPF_PROG_TYPE_STRUCT_OPS                   = 0x1b
	BPF_PROG_TYPE_EXT                          = 0x1c
	BPF_PROG_TYPE_LSM                          = 0x1d
	BPF_PROG_TYPE_SK_LOOKUP                    = 0x1e
	BPF_PROG_TYPE_SYSCALL                      = 0x1f
	BPF_PROG_TYPE_NETFILTER                    = 0x20
	BPF_CGROUP_INET_INGRESS                    = 0x0
	BPF_CGROUP_INET_EGRESS                     = 0x1
	BPF_CGROUP_INET_SOCK_CREATE                = 0x2
	BPF_CGROUP_SOCK_OPS                        = 0x3
	BPF_SK_SKB_STREAM_PARSER                   = 0x4
	BPF_SK_SKB_STREAM_VERDICT                  = 0x5
	BPF_CGROUP_DEVICE                          = 0x6
	BPF_SK_MSG_VERDICT                         = 0x7
	BPF_CGROUP_INET4_BIND                      = 0x8
	BPF_CGROUP_INET6_BIND                      = 0x9
	BPF_CGROUP_INET4_CONNECT                   = 0xa
	BPF_CGROUP_INET6_CONNECT                   = 0xb
	BPF_CGROUP_INET4_POST_BIND                 = 0xc
	BPF_CGROUP_INET6_POST_BIND                 = 0xd
	BPF_CGROUP_UDP4_SENDMSG                    = 0xe
	BPF_CGROUP_UDP6_SENDMSG                    = 0xf
	BPF_LIRC_MODE2                             = 0x10
	BPF_FLOW_DISSECTOR                         = 0x11
	BPF_CGROUP_SYSCTL                          = 0x12
	BPF_CGROUP_UDP4_RECVMSG                    = 0x13
	BPF_CGROUP_UDP6_RECVMSG                    = 0x14
	BPF_CGROUP_GETSOCKOPT                      = 0x15
	BPF_CGROUP_SETSOCKOPT                      = 0x16
	BPF_TRACE_RAW_TP                           = 0x17
	BPF_TRACE_FENTRY                           = 0x18
	BPF_TRACE_FEXIT                            = 0x19
	BPF_MODIFY_RETURN                          = 0x1a
	BPF_LSM_MAC                                = 0x1b
	BPF_TRACE_ITER                             = 0x1c
	BPF_CGROUP_INET4_GETPEERNAME               = 0x1d
	BPF_CGROUP_INET6_GETPEERNAME               = 0x1e
	BPF_CGROUP_INET4_GETSOCKNAME               = 0x1f
	BPF_CGROUP_INET6_GETSOCKNAME               = 0x20
	BPF_XDP_DEVMAP                             = 0x21
	BPF_CGROUP_INET_SOCK_RELEASE               = 0x22
	BPF_XDP_CPUMAP                             = 0x23
	BPF_SK_LOOKUP                              = 0x24
	BPF_XDP                                    = 0x25
	BPF_SK_SKB_VERDICT                         = 0x26
	BPF_SK_REUSEPORT_SELECT                    = 0x27
	BPF_SK_REUSEPORT_SELECT_OR_MIGRATE         = 0x28
	BPF_PERF_EVENT                             = 0x29
	BPF_TRACE_KPROBE_MULTI                     = 0x2a
	BPF_LSM_CGROUP                             = 0x2b
	BPF_STRUCT_OPS                             = 0x2c
	BPF_NETFILTER                              = 0x2d
	BPF_TCX_INGRESS                            = 0x2e
	BPF_TCX_EGRESS                             = 0x2f
	BPF_TRACE_UPROBE_MULTI                     = 0x30
	BPF_LINK_TYPE_UNSPEC                       = 0x0
	BPF_LINK_TYPE_RAW_TRACEPOINT               = 0x1
	BPF_LINK_TYPE_TRACING                      = 0x2
	BPF_LINK_TYPE_CGROUP                       = 0x3
	BPF_LINK_TYPE_ITER                         = 0x4
	BPF_LINK_TYPE_NETNS                        = 0x5
	BPF_LINK_TYPE_XDP                          = 0x6
	BPF_LINK_TYPE_PERF_EVENT                   = 0x7
	BPF_LINK_TYPE_KPROBE_MULTI                 = 0x8
	BPF_LINK_TYPE_STRUCT_OPS                   = 0x9
	BPF_LINK_TYPE_NETFILTER                    = 0xa
	BPF_LINK_TYPE_TCX                          = 0xb
	BPF_LINK_TYPE_UPROBE_MULTI                 = 0xc
	BPF_PERF_EVENT_UNSPEC                      = 0x0
	BPF_PERF_EVENT_UPROBE                      = 0x1
	BPF_PERF_EVENT_URETPROBE                   = 0x2
	BPF_PERF_EVENT_KPROBE                      = 0x3
	BPF_PERF_EVENT_KRETPROBE                   = 0x4
	BPF_PERF_EVENT_TRACEPOINT                  = 0x5
	BPF_PERF_EVENT_EVENT                       = 0x6
	BPF_F_KPROBE_MULTI_RETURN                  = 0x1
	BPF_F_UPROBE_MULTI_RETURN                  = 0x1
	BPF_ANY                                    = 0x0
	BPF_NOEXIST                                = 0x1
	BPF_EXIST                                  = 0x2
	BPF_F_LOCK                                 = 0x4
	BPF_F_NO_PREALLOC                          = 0x1
	BPF_F_NO_COMMON_LRU                        = 0x2
	BPF_F_NUMA_NODE                            = 0x4
	BPF_F_RDONLY                               = 0x8
	BPF_F_WRONLY                               = 0x10
	BPF_F_STACK_BUILD_ID                       = 0x20
	BPF_F_ZERO_SEED                            = 0x40
	BPF_F_RDONLY_PROG                          = 0x80
	BPF_F_WRONLY_PROG                          = 0x100
	BPF_F_CLONE                                = 0x200
	BPF_F_MMAPABLE                             = 0x400
	BPF_F_PRESERVE_ELEMS                       = 0x800
	BPF_F_INNER_MAP                            = 0x1000
	BPF_F_LINK                                 = 0x2000
	BPF_F_PATH_FD                              = 0x4000
	BPF_STATS_RUN_TIME                         = 0x0
	BPF_STACK_BUILD_ID_EMPTY                   = 0x0
	BPF_STACK_BUILD_ID_VALID                   = 0x1
	BPF_STACK_BUILD_ID_IP                      = 0x2
	BPF_F_RECOMPUTE_CSUM                       = 0x1
	BPF_F_INVALIDATE_HASH                      = 0x2
	BPF_F_HDR_FIELD_MASK                       = 0xf
	BPF_F_PSEUDO_HDR                           = 0x10
	BPF_F_MARK_MANGLED_0                       = 0x20
	BPF_F_MARK_ENFORCE                         = 0x40
	BPF_F_INGRESS                              = 0x1
	BPF_F_TUNINFO_IPV6                         = 0x1
	BPF_F_SKIP_FIELD_MASK                      = 0xff
	BPF_F_USER_STACK                           = 0x100
	BPF_F_FAST_STACK_CMP                       = 0x200
	BPF_F_REUSE_STACKID                        = 0x400
	BPF_F_USER_BUILD_ID                        = 0x800
	BPF_F_ZERO_CSUM_TX                         = 0x2
	BPF_F_DONT_FRAGMENT                        = 0x4
	BPF_F_SEQ_NUMBER                           = 0x8
	BPF_F_NO_TUNNEL_KEY                        = 0x10
	BPF_F_TUNINFO_FLAGS                        = 0x10
	BPF_F_INDEX_MASK                           = 0xffffffff
	BPF_F_CURRENT_CPU                          = 0xffffffff
	BPF_F_CTXLEN_MASK                          = 0xfffff00000000
	BPF_F_CURRENT_NETNS                        = -0x1
	BPF_CSUM_LEVEL_QUERY                       = 0x0
	BPF_CSUM_LEVEL_INC                         = 0x1
	BPF_CSUM_LEVEL_DEC                         = 0x2
	BPF_CSUM_LEVEL_RESET                       = 0x3
	BPF_F_ADJ_ROOM_FIXED_GSO                   = 0x1
	BPF_F_ADJ_ROOM_ENCAP_L3_IPV4               = 0x2
	BPF_F_ADJ_ROOM_ENCAP_L3_IPV6               = 0x4
	BPF_F_ADJ_ROOM_ENCAP_L4_GRE                = 0x8
	BPF_F_ADJ_ROOM_ENCAP_L4_UDP                = 0x10
	BPF_F_ADJ_ROOM_NO_CSUM_RESET               = 0x20
	BPF_F_ADJ_ROOM_ENCAP_L2_ETH                = 0x40
	BPF_F_ADJ_ROOM_DECAP_L3_IPV4               = 0x80
	BPF_F_ADJ_ROOM_DECAP_L3_IPV6               = 0x100
	BPF_ADJ_ROOM_ENCAP_L2_MASK                 = 0xff
	BPF_ADJ_ROOM_ENCAP_L2_SHIFT                = 0x38
	BPF_F_SYSCTL_BASE_NAME                     = 0x1
	BPF_LOCAL_STORAGE_GET_F_CREATE             = 0x1
	BPF_SK_STORAGE_GET_F_CREATE                = 0x1
	BPF_F_GET_BRANCH_RECORDS_SIZE              = 0x1
	BPF_RB_NO_WAKEUP                           = 0x1
	BPF_RB_FORCE_WAKEUP                        = 0x2
	BPF_RB_AVAIL_DATA                          = 0x0
	BPF_RB_RING_SIZE                           = 0x1
	BPF_RB_CONS_POS                            = 0x2
	BPF_RB_PROD_POS                            = 0x3
	BPF_RINGBUF_BUSY_BIT                       = 0x80000000
	BPF_RINGBUF_DISCARD_BIT                    = 0x40000000
	BPF_RINGBUF_HDR_SZ                         = 0x8
	BPF_SK_LOOKUP_F_REPLACE                    = 0x1
	BPF_SK_LOOKUP_F_NO_REUSEPORT               = 0x2
	BPF_ADJ_ROOM_NET                           = 0x0
	BPF_ADJ_ROOM_MAC                           = 0x1
	BPF_HDR_START_MAC                          = 0x0
	BPF_HDR_START_NET                          = 0x1
	BPF_LWT_ENCAP_SEG6                         = 0x0
	BPF_LWT_ENCAP_SEG6_INLINE                  = 0x1
	BPF_LWT_ENCAP_IP                           = 0x2
	BPF_F_BPRM_SECUREEXEC                      = 0x1
	BPF_F_BROADCAST                            = 0x8
	BPF_F_EXCLUDE_INGRESS                      = 0x10
	BPF_SKB_TSTAMP_UNSPEC                      = 0x0
	BPF_SKB_TSTAMP_DELIVERY_MONO               = 0x1
	BPF_OK                                     = 0x0
	BPF_DROP                                   = 0x2
	BPF_REDIRECT                               = 0x7
	BPF_LWT_REROUTE                            = 0x80
	BPF_FLOW_DISSECTOR_CONTINUE                = 0x81
	BPF_SOCK_OPS_RTO_CB_FLAG                   = 0x1
	BPF_SOCK_OPS_RETRANS_CB_FLAG               = 0x2
	BPF_SOCK_OPS_STATE_CB_FLAG                 = 0x4
	BPF_SOCK_OPS_RTT_CB_FLAG                   = 0x8
	BPF_SOCK_OPS_PARSE_ALL_HDR_OPT_CB_FLAG     = 0x10
	BPF_SOCK_OPS_PARSE_UNKNOWN_HDR_OPT_CB_FLAG = 0x20
	BPF_SOCK_OPS_WRITE_HDR_OPT_CB_FLAG         = 0x40
	BPF_SOCK_OPS_ALL_CB_FLAGS                  = 0x7f
	BPF_SOCK_OPS_VOID                          = 0x0
	BPF_SOCK_OPS_TIMEOUT_INIT                  = 0x1
	BPF_SOCK_OPS_RWND_INIT                     = 0x2
	BPF_SOCK_OPS_TCP_CONNECT_CB                = 0x3
	BPF_SOCK_OPS_ACTIVE_ESTABLISHED_CB         = 0x4
	BPF_SOCK_OPS_PASSIVE_ESTABLISHED_CB        = 0x5
	BPF_SOCK_OPS_NEEDS_ECN                     = 0x6
	BPF_SOCK_OPS_BASE_RTT                      = 0x7
	BPF_SOCK_OPS_RTO_CB                        = 0x8
	BPF_SOCK_OPS_RETRANS_CB                    = 0x9
	BPF_SOCK_OPS_STATE_CB                      = 0xa
	BPF_SOCK_OPS_TCP_LISTEN_CB                 = 0xb
	BPF_SOCK_OPS_RTT_CB                        = 0xc
	BPF_SOCK_OPS_PARSE_HDR_OPT_CB              = 0xd
	BPF_SOCK_OPS_HDR_OPT_LEN_CB                = 0xe
	BPF_SOCK_OPS_WRITE_HDR_OPT_CB              = 0xf
	BPF_TCP_ESTABLISHED                        = 0x1
	BPF_TCP_SYN_SENT                           = 0x2
	BPF_TCP_SYN_RECV                           = 0x3
	BPF_TCP_FIN_WAIT1                          = 0x4
	BPF_TCP_FIN_WAIT2                          = 0x5
	BPF_TCP_TIME_WAIT                          = 0x6
	BPF_TCP_CLOSE                              = 0x7
	BPF_TCP_CLOSE_WAIT                         = 0x8
	BPF_TCP_LAST_ACK                           = 0x9
	BPF_TCP_LISTEN                             = 0xa
	BPF_TCP_CLOSING                            = 0xb
	BPF_TCP_NEW_SYN_RECV                       = 0xc
	BPF_TCP_MAX_STATES                         = 0xe
	TCP_BPF_IW                                 = 0x3e9
	TCP_BPF_SNDCWND_CLAMP                      = 0x3ea
	TCP_BPF_DELACK_MAX                         = 0x3eb
	TCP_BPF_RTO_MIN                            = 0x3ec
	TCP_BPF_SYN                                = 0x3ed
	TCP_BPF_SYN_IP                             = 0x3ee
	TCP_BPF_SYN_MAC                            = 0x3ef
	BPF_LOAD_HDR_OPT_TCP_SYN                   = 0x1
	BPF_WRITE_HDR_TCP_CURRENT_MSS              = 0x1
	BPF_WRITE_HDR_TCP_SYNACK_COOKIE            = 0x2
	BPF_DEVCG_ACC_MKNOD                        = 0x1
	BPF_DEVCG_ACC_READ                         = 0x2
	BPF_DEVCG_ACC_WRITE                        = 0x4
	BPF_DEVCG_DEV_BLOCK                        = 0x1
	BPF_DEVCG_DEV_CHAR                         = 0x2
	BPF_FIB_LOOKUP_DIRECT                      = 0x1
	BPF_FIB_LOOKUP_OUTPUT                      = 0x2
	BPF_FIB_LOOKUP_SKIP_NEIGH                  = 0x4
	BPF_FIB_LOOKUP_TBID                        = 0x8
	BPF_FIB_LKUP_RET_SUCCESS                   = 0x0
	BPF_FIB_LKUP_RET_BLACKHOLE                 = 0x1
	BPF_FIB_LKUP_RET_UNREACHABLE               = 0x2
	BPF_FIB_LKUP_RET_PROHIBIT                  = 0x3
	BPF_FIB_LKUP_RET_NOT_FWDED                 = 0x4
	BPF_FIB_LKUP_RET_FWD_DISABLED              = 0x5
	BPF_FIB_LKUP_RET_UNSUPP_LWT                = 0x6
	BPF_FIB_LKUP_RET_NO_NEIGH                  = 0x7
	BPF_FIB_LKUP_RET_FRAG_NEEDED               = 0x8
	BPF_MTU_CHK_SEGS                           = 0x1
	BPF_MTU_CHK_RET_SUCCESS                    = 0x0
	BPF_MTU_CHK_RET_FRAG_NEEDED                = 0x1
	BPF_MTU_CHK_RET_SEGS_TOOBIG                = 0x2
	BPF_FD_TYPE_RAW_TRACEPOINT                 = 0x0
	BPF_FD_TYPE_TRACEPOINT                     = 0x1
	BPF_FD_TYPE_KPROBE                         = 0x2
	BPF_FD_TYPE_KRETPROBE                      = 0x3
	BPF_FD_TYPE_UPROBE                         = 0x4
	BPF_FD_TYPE_URETPROBE                      = 0x5
	BPF_FLOW_DISSECTOR_F_PARSE_1ST_FRAG        = 0x1
	BPF_FLOW_DISSECTOR_F_STOP_AT_FLOW_LABEL    = 0x2
	BPF_FLOW_DISSECTOR_F_STOP_AT_ENCAP         = 0x4
	BPF_CORE_FIELD_BYTE_OFFSET                 = 0x0
	BPF_CORE_FIELD_BYTE_SIZE                   = 0x1
	BPF_CORE_FIELD_EXISTS                      = 0x2
	BPF_CORE_FIELD_SIGNED                      = 0x3
	BPF_CORE_FIELD_LSHIFT_U64                  = 0x4
	BPF_CORE_FIELD_RSHIFT_U64                  = 0x5
	BPF_CORE_TYPE_ID_LOCAL                     = 0x6
	BPF_CORE_TYPE_ID_TARGET                    = 0x7
	BPF_CORE_TYPE_EXISTS                       = 0x8
	BPF_CORE_TYPE_SIZE                         = 0x9
	BPF_CORE_ENUMVAL_EXISTS                    = 0xa
	BPF_CORE_ENUMVAL_VALUE                     = 0xb
	BPF_CORE_TYPE_MATCHES                      = 0xc
	BPF_F_TIMER_ABS                            = 0x1
)

const (
	TCA_UNSPEC            = 0x0
	TCA_KIND              = 0x1
	TCA_OPTIONS           = 0x2
	TCA_STATS             = 0x3
	TCA_XSTATS            = 0x4
	TCA_RATE              = 0x5
	TCA_FCNT              = 0x6
	TCA_STATS2            = 0x7
	TCA_STAB              = 0x8
	TCA_PAD               = 0x9
	TCA_DUMP_INVISIBLE    = 0xa
	TCA_CHAIN             = 0xb
	TCA_HW_OFFLOAD        = 0xc
	TCA_INGRESS_BLOCK     = 0xd
	TCA_EGRESS_BLOCK      = 0xe
	TCA_DUMP_FLAGS        = 0xf
	TCA_EXT_WARN_MSG      = 0x10
	RTNLGRP_NONE          = 0x0
	RTNLGRP_LINK          = 0x1
	RTNLGRP_NOTIFY        = 0x2
	RTNLGRP_NEIGH         = 0x3
	RTNLGRP_TC            = 0x4
	RTNLGRP_IPV4_IFADDR   = 0x5
	RTNLGRP_IPV4_MROUTE   = 0x6
	RTNLGRP_IPV4_ROUTE    = 0x7
	RTNLGRP_IPV4_RULE     = 0x8
	RTNLGRP_IPV6_IFADDR   = 0x9
	RTNLGRP_IPV6_MROUTE   = 0xa
	RTNLGRP_IPV6_ROUTE    = 0xb
	RTNLGRP_IPV6_IFINFO   = 0xc
	RTNLGRP_DECnet_IFADDR = 0xd
	RTNLGRP_NOP2          = 0xe
	RTNLGRP_DECnet_ROUTE  = 0xf
	RTNLGRP_DECnet_RULE   = 0x10
	RTNLGRP_NOP4          = 0x11
	RTNLGRP_IPV6_PREFIX   = 0x12
	RTNLGRP_IPV6_RULE     = 0x13
	RTNLGRP_ND_USEROPT    = 0x14
	RTNLGRP_PHONET_IFADDR = 0x15
	RTNLGRP_PHONET_ROUTE  = 0x16
	RTNLGRP_DCB           = 0x17
	RTNLGRP_IPV4_NETCONF  = 0x18
	RTNLGRP_IPV6_NETCONF  = 0x19
	RTNLGRP_MDB           = 0x1a
	RTNLGRP_MPLS_ROUTE    = 0x1b
	RTNLGRP_NSID          = 0x1c
	RTNLGRP_MPLS_NETCONF  = 0x1d
	RTNLGRP_IPV4_MROUTE_R = 0x1e
	RTNLGRP_IPV6_MROUTE_R = 0x1f
	RTNLGRP_NEXTHOP       = 0x20
	RTNLGRP_BRVLAN        = 0x21
	RTNLGRP_MCTP_IFADDR   = 0x22
	RTNLGRP_TUNNEL        = 0x23
	RTNLGRP_STATS         = 0x24
	RTNLGRP_IPV4_MCADDR   = 0x25
	RTNLGRP_IPV6_MCADDR   = 0x26
	RTNLGRP_IPV6_ACADDR   = 0x27
	TCA_ROOT_UNSPEC       = 0x0
	TCA_ROOT_TAB          = 0x1
	TCA_ROOT_FLAGS        = 0x2
	TCA_ROOT_COUNT        = 0x3
	TCA_ROOT_TIME_DELTA   = 0x4
	TCA_ROOT_EXT_WARN_MSG = 0x5
)

type CapUserHeader struct {
	Version uint32
	Pid     int32
}

type CapUserData struct {
	Effective   uint32
	Permitted   uint32
	Inheritable uint32
}

const (
	LINUX_CAPABILITY_VERSION_1 = 0x19980330
	LINUX_CAPABILITY_VERSION_2 = 0x20071026
	LINUX_CAPABILITY_VERSION_3 = 0x20080522
)

const (
	LO_FLAGS_READ_ONLY = 0x1
	LO_FLAGS_AUTOCLEAR = 0x4
	LO_FLAGS_PARTSCAN  = 0x8
	LO_FLAGS_DIRECT_IO = 0x10
)

type LoopInfo64 struct {
	Device           uint64
	Inode            uint64
	Rdevice          uint64
	Offset           uint64
	Sizelimit        uint64
	Number           uint32
	Encrypt_type     uint32
	Encrypt_key_size uint32
	Flags            uint32
	File_name        [64]uint8
	Crypt_name       [64]uint8
	Encrypt_key      [32]uint8
	Init             [2]uint64
}
type LoopConfig struct {
	Fd   uint32
	Size uint32
	Info LoopInfo64
	_    [8]uint64
}

type TIPCSocketAddr struct {
	Ref  uint32
	Node uint32
}

type TIPCServiceRange struct {
	Type  uint32
	Lower uint32
	Upper uint32
}

type TIPCServiceName struct {
	Type     uint32
	Instance uint32
	Domain   uint32
}

type TIPCEvent struct {
	Event uint32
	Lower uint32
	Upper uint32
	Port  TIPCSocketAddr
	S     TIPCSubscr
}

type TIPCGroupReq struct {
	Type     uint32
	Instance uint32
	Scope    uint32
	Flags    uint32
}

const (
	TIPC_CLUSTER_SCOPE = 0x2
	TIPC_NODE_SCOPE    = 0x3
)

const (
	SYSLOG_ACTION_CLOSE         = 0
	SYSLOG_ACTION_OPEN          = 1
	SYSLOG_ACTION_READ          = 2
	SYSLOG_ACTION_READ_ALL      = 3
	SYSLOG_ACTION_READ_CLEAR    = 4
	SYSLOG_ACTION_CLEAR         = 5
	SYSLOG_ACTION_CONSOLE_OFF   = 6
	SYSLOG_ACTION_CONSOLE_ON    = 7
	SYSLOG_ACTION_CONSOLE_LEVEL = 8
	SYSLOG_ACTION_SIZE_UNREAD   = 9
	SYSLOG_ACTION_SIZE_BUFFER   = 10
)

const (
	DEVLINK_CMD_UNSPEC                                 = 0x0
	DEVLINK_CMD_GET                                    = 0x1
	DEVLINK_CMD_SET                                    = 0x2
	DEVLINK_CMD_NEW                                    = 0x3
	DEVLINK_CMD_DEL                                    = 0x4
	DEVLINK_CMD_PORT_GET                               = 0x5
	DEVLINK_CMD_PORT_SET                               = 0x6
	DEVLINK_CMD_PORT_NEW                               = 0x7
	DEVLINK_CMD_PORT_DEL                               = 0x8
	DEVLINK_CMD_PORT_SPLIT                             = 0x9
	DEVLINK_CMD_PORT_UNSPLIT                           = 0xa
	DEVLINK_CMD_SB_GET                                 = 0xb
	DEVLINK_CMD_SB_SET                                 = 0xc
	DEVLINK_CMD_SB_NEW                                 = 0xd
	DEVLINK_CMD_SB_DEL                                 = 0xe
	DEVLINK_CMD_SB_POOL_GET                            = 0xf
	DEVLINK_CMD_SB_POOL_SET                            = 0x10
	DEVLINK_CMD_SB_POOL_NEW                            = 0x11
	DEVLINK_CMD_SB_POOL_DEL                            = 0x12
	DEVLINK_CMD_SB_PORT_POOL_GET                       = 0x13
	DEVLINK_CMD_SB_PORT_POOL_SET                       = 0x14
	DEVLINK_CMD_SB_PORT_POOL_NEW                       = 0x15
	DEVLINK_CMD_SB_PORT_POOL_DEL                       = 0x16
	DEVLINK_CMD_SB_TC_POOL_BIND_GET                    = 0x17
	DEVLINK_CMD_SB_TC_POOL_BIND_SET                    = 0x18
	DEVLINK_CMD_SB_TC_POOL_BIND_NEW                    = 0x19
	DEVLINK_CMD_SB_TC_POOL_BIND_DEL                    = 0x1a
	DEVLINK_CMD_SB_OCC_SNAPSHOT                        = 0x1b
	DEVLINK_CMD_SB_OCC_MAX_CLEAR                       = 0x1c
	DEVLINK_CMD_ESWITCH_GET                            = 0x1d
	DEVLINK_CMD_ESWITCH_SET                            = 0x1e
	DEVLINK_CMD_DPIPE_TABLE_GET                        = 0x1f
	DEVLINK_CMD_DPIPE_ENTRIES_GET                      = 0x20
	DEVLINK_CMD_DPIPE_HEADERS_GET                      = 0x21
	DEVLINK_CMD_DPIPE_TABLE_COUNTERS_SET               = 0x22
	DEVLINK_CMD_RESOURCE_SET                           = 0x23
	DEVLINK_CMD_RESOURCE_DUMP                          = 0x24
	DEVLINK_CMD_RELOAD                                 = 0x25
	DEVLINK_CMD_PARAM_GET                              = 0x26
	DEVLINK_CMD_PARAM_SET                              = 0x27
	DEVLINK_CMD_PARAM_NEW                              = 0x28
	DEVLINK_CMD_PARAM_DEL                              = 0x29
	DEVLINK_CMD_REGION_GET                             = 0x2a
	DEVLINK_CMD_REGION_SET                             = 0x2b
	DEVLINK_CMD_REGION_NEW                             = 0x2c
	DEVLINK_CMD_REGION_DEL                             = 0x2d
	DEVLINK_CMD_REGION_READ                            = 0x2e
	DEVLINK_CMD_PORT_PARAM_GET                         = 0x2f
	DEVLINK_CMD_PORT_PARAM_SET                         = 0x30
	DEVLINK_CMD_PORT_PARAM_NEW                         = 0x31
	DEVLINK_CMD_PORT_PARAM_DEL                         = 0x32
	DEVLINK_CMD_INFO_GET                               = 0x33
	DEVLINK_CMD_HEALTH_REPORTER_GET                    = 0x34
	DEVLINK_CMD_HEALTH_REPORTER_SET                    = 0x35
	DEVLINK_CMD_HEALTH_REPORTER_RECOVER                = 0x36
	DEVLINK_CMD_HEALTH_REPORTER_DIAGNOSE               = 0x37
	DEVLINK_CMD_HEALTH_REPORTER_DUMP_GET               = 0x38
	DEVLINK_CMD_HEALTH_REPORTER_DUMP_CLEAR             = 0x39
	DEVLINK_CMD_FLASH_UPDATE                           = 0x3a
	DEVLINK_CMD_FLASH_UPDATE_END                       = 0x3b
	DEVLINK_CMD_FLASH_UPDATE_STATUS                    = 0x3c
	DEVLINK_CMD_TRAP_GET                               = 0x3d
	DEVLINK_CMD_TRAP_SET                               = 0x3e
	DEVLINK_CMD_TRAP_NEW                               = 0x3f
	DEVLINK_CMD_TRAP_DEL                               = 0x40
	DEVLINK_CMD_TRAP_GROUP_GET                         = 0x41
	DEVLINK_CMD_TRAP_GROUP_SET                         = 0x42
	DEVLINK_CMD_TRAP_GROUP_NEW                         = 0x43
	DEVLINK_CMD_TRAP_GROUP_DEL                         = 0x44
	DEVLINK_CMD_TRAP_POLICER_GET                       = 0x45
	DEVLINK_CMD_TRAP_POLICER_SET                       = 0x46
	DEVLINK_CMD_TRAP_POLICER_NEW                       = 0x47
	DEVLINK_CMD_TRAP_POLICER_DEL                       = 0x48
	DEVLINK_CMD_HEALTH_REPORTER_TEST                   = 0x49
	DEVLINK_CMD_RATE_GET                               = 0x4a
	DEVLINK_CMD_RATE_SET                               = 0x4b
	DEVLINK_CMD_RATE_NEW                               = 0x4c
	DEVLINK_CMD_RATE_DEL                               = 0x4d
	DEVLINK_CMD_LINECARD_GET                           = 0x4e
	DEVLINK_CMD_LINECARD_SET                           = 0x4f
	DEVLINK_CMD_LINECARD_NEW                           = 0x50
	DEVLINK_CMD_LINECARD_DEL                           = 0x51
	DEVLINK_CMD_SELFTESTS_GET                          = 0x52
	DEVLINK_CMD_MAX                                    = 0x54
	DEVLINK_PORT_TYPE_NOTSET                           = 0x0
	DEVLINK_PORT_TYPE_AUTO                             = 0x1
	DEVLINK_PORT_TYPE_ETH                              = 0x2
	DEVLINK_PORT_TYPE_IB                               = 0x3
	DEVLINK_SB_POOL_TYPE_INGRESS                       = 0x0
	DEVLINK_SB_POOL_TYPE_EGRESS                        = 0x1
	DEVLINK_SB_THRESHOLD_TYPE_STATIC                   = 0x0
	DEVLINK_SB_THRESHOLD_TYPE_DYNAMIC                  = 0x1
	DEVLINK_ESWITCH_MODE_LEGACY                        = 0x0
	DEVLINK_ESWITCH_MODE_SWITCHDEV                     = 0x1
	DEVLINK_ESWITCH_INLINE_MODE_NONE                   = 0x0
	DEVLINK_ESWITCH_INLINE_MODE_LINK                   = 0x1
	DEVLINK_ESWITCH_INLINE_MODE_NETWORK                = 0x2
	DEVLINK_ESWITCH_INLINE_MODE_TRANSPORT              = 0x3
	DEVLINK_ESWITCH_ENCAP_MODE_NONE                    = 0x0
	DEVLINK_ESWITCH_ENCAP_MODE_BASIC                   = 0x1
	DEVLINK_PORT_FLAVOUR_PHYSICAL                      = 0x0
	DEVLINK_PORT_FLAVOUR_CPU                           = 0x1
	DEVLINK_PORT_FLAVOUR_DSA                           = 0x2
	DEVLINK_PORT_FLAVOUR_PCI_PF                        = 0x3
	DEVLINK_PORT_FLAVOUR_PCI_VF                        = 0x4
	DEVLINK_PORT_FLAVOUR_VIRTUAL                       = 0x5
	DEVLINK_PORT_FLAVOUR_UNUSED                        = 0x6
	DEVLINK_PARAM_CMODE_RUNTIME                        = 0x0
	DEVLINK_PARAM_CMODE_DRIVERINIT                     = 0x1
	DEVLINK_PARAM_CMODE_PERMANENT                      = 0x2
	DEVLINK_PARAM_CMODE_MAX                            = 0x2
	DEVLINK_PARAM_FW_LOAD_POLICY_VALUE_DRIVER          = 0x0
	DEVLINK_PARAM_FW_LOAD_POLICY_VALUE_FLASH           = 0x1
	DEVLINK_PARAM_FW_LOAD_POLICY_VALUE_DISK            = 0x2
	DEVLINK_PARAM_FW_LOAD_POLICY_VALUE_UNKNOWN         = 0x3
	DEVLINK_PARAM_RESET_DEV_ON_DRV_PROBE_VALUE_UNKNOWN = 0x0
	DEVLINK_PARAM_RESET_DEV_ON_DRV_PROBE_VALUE_ALWAYS  = 0x1
	DEVLINK_PARAM_RESET_DEV_ON_DRV_PROBE_VALUE_NEVER   = 0x2
	DEVLINK_PARAM_RESET_DEV_ON_DRV_PROBE_VALUE_DISK    = 0x3
	DEVLINK_ATTR_STATS_RX_PACKETS                      = 0x0
	DEVLINK_ATTR_STATS_RX_BYTES                        = 0x1
	DEVLINK_ATTR_STATS_RX_DROPPED                      = 0x2
	DEVLINK_ATTR_STATS_MAX                             = 0x2
	DEVLINK_FLASH_OVERWRITE_SETTINGS_BIT               = 0x0
	DEVLINK_FLASH_OVERWRITE_IDENTIFIERS_BIT            = 0x1
	DEVLINK_FLASH_OVERWRITE_MAX_BIT                    = 0x1
	DEVLINK_TRAP_ACTION_DROP                           = 0x0
	DEVLINK_TRAP_ACTION_TRAP                           = 0x1
	DEVLINK_TRAP_ACTION_MIRROR                         = 0x2
	DEVLINK_TRAP_TYPE_DROP                             = 0x0
	DEVLINK_TRAP_TYPE_EXCEPTION                        = 0x1
	DEVLINK_TRAP_TYPE_CONTROL                          = 0x2
	DEVLINK_ATTR_TRAP_METADATA_TYPE_IN_PORT            = 0x0
	DEVLINK_ATTR_TRAP_METADATA_TYPE_FA_COOKIE          = 0x1
	DEVLINK_RELOAD_ACTION_UNSPEC                       = 0x0
	DEVLINK_RELOAD_ACTION_DRIVER_REINIT                = 0x1
	DEVLINK_RELOAD_ACTION_FW_ACTIVATE                  = 0x2
	DEVLINK_RELOAD_ACTION_MAX                          = 0x2
	DEVLINK_RELOAD_LIMIT_UNSPEC                        = 0x0
	DEVLINK_RELOAD_LIMIT_NO_RESET                      = 0x1
	DEVLINK_RELOAD_LIMIT_MAX                           = 0x1
	DEVLINK_ATTR_UNSPEC                                = 0x0
	DEVLINK_ATTR_BUS_NAME                              = 0x1
	DEVLINK_ATTR_DEV_NAME                              = 0x2
	DEVLINK_ATTR_PORT_INDEX                            = 0x3
	DEVLINK_ATTR_PORT_TYPE                             = 0x4
	DEVLINK_ATTR_PORT_DESIRED_TYPE                     = 0x5
	DEVLINK_ATTR_PORT_NETDEV_IFINDEX                   = 0x6
	DEVLINK_ATTR_PORT_NETDEV_NAME                      = 0x7
	DEVLINK_ATTR_PORT_IBDEV_NAME                       = 0x8
	DEVLINK_ATTR_PORT_SPLIT_COUNT                      = 0x9
	DEVLINK_ATTR_PORT_SPLIT_GROUP                      = 0xa
	DEVLINK_ATTR_SB_INDEX                              = 0xb
	DEVLINK_ATTR_SB_SIZE                               = 0xc
	DEVLINK_ATTR_SB_INGRESS_POOL_COUNT                 = 0xd
	DEVLINK_ATTR_SB_EGRESS_POOL_COUNT                  = 0xe
	DEVLINK_ATTR_SB_INGRESS_TC_COUNT                   = 0xf
	DEVLINK_ATTR_SB_EGRESS_TC_COUNT                    = 0x10
	DEVLINK_ATTR_SB_POOL_INDEX                         = 0x11
	DEVLINK_ATTR_SB_POOL_TYPE                          = 0x12
	DEVLINK_ATTR_SB_POOL_SIZE                          = 0x13
	DEVLINK_ATTR_SB_POOL_THRESHOLD_TYPE                = 0x14
	DEVLINK_ATTR_SB_THRESHOLD                          = 0x15
	DEVLINK_ATTR_SB_TC_INDEX                           = 0x16
	DEVLINK_ATTR_SB_OCC_CUR                            = 0x17
	DEVLINK_ATTR_SB_OCC_MAX                            = 0x18
	DEVLINK_ATTR_ESWITCH_MODE                          = 0x19
	DEVLINK_ATTR_ESWITCH_INLINE_MODE                   = 0x1a
	DEVLINK_ATTR_DPIPE_TABLES                          = 0x1b
	DEVLINK_ATTR_DPIPE_TABLE                           = 0x1c
	DEVLINK_ATTR_DPIPE_TABLE_NAME                      = 0x1d
	DEVLINK_ATTR_DPIPE_TABLE_SIZE                      = 0x1e
	DEVLINK_ATTR_DPIPE_TABLE_MATCHES                   = 0x1f
	DEVLINK_ATTR_DPIPE_TABLE_ACTIONS                   = 0x20
	DEVLINK_ATTR_DPIPE_TABLE_COUNTERS_ENABLED          = 0x21
	DEVLINK_ATTR_DPIPE_ENTRIES                         = 0x22
	DEVLINK_ATTR_DPIPE_ENTRY                           = 0x23
	DEVLINK_ATTR_DPIPE_ENTRY_INDEX                     = 0x24
	DEVLINK_ATTR_DPIPE_ENTRY_MATCH_VALUES              = 0x25
	DEVLINK_ATTR_DPIPE_ENTRY_ACTION_VALUES             = 0x26
	DEVLINK_ATTR_DPIPE_ENTRY_COUNTER                   = 0x27
	DEVLINK_ATTR_DPIPE_MATCH                           = 0x28
	DEVLINK_ATTR_DPIPE_MATCH_VALUE                     = 0x29
	DEVLINK_ATTR_DPIPE_MATCH_TYPE                      = 0x2a
	DEVLINK_ATTR_DPIPE_ACTION                          = 0x2b
	DEVLINK_ATTR_DPIPE_ACTION_VALUE                    = 0x2c
	DEVLINK_ATTR_DPIPE_ACTION_TYPE                     = 0x2d
	DEVLINK_ATTR_DPIPE_VALUE                           = 0x2e
	DEVLINK_ATTR_DPIPE_VALUE_MASK                      = 0x2f
	DEVLINK_ATTR_DPIPE_VALUE_MAPPING                   = 0x30
	DEVLINK_ATTR_DPIPE_HEADERS                         = 0x31
	DEVLINK_ATTR_DPIPE_HEADER                          = 0x32
	DEVLINK_ATTR_DPIPE_HEADER_NAME                     = 0x33
	DEVLINK_ATTR_DPIPE_HEADER_ID                       = 0x34
	DEVLINK_ATTR_DPIPE_HEADER_FIELDS                   = 0x35
	DEVLINK_ATTR_DPIPE_HEADER_GLOBAL                   = 0x36
	DEVLINK_ATTR_DPIPE_HEADER_INDEX                    = 0x37
	DEVLINK_ATTR_DPIPE_FIELD                           = 0x38
	DEVLINK_ATTR_DPIPE_FIELD_NAME                      = 0x39
	DEVLINK_ATTR_DPIPE_FIELD_ID                        = 0x3a
	DEVLINK_ATTR_DPIPE_FIELD_BITWIDTH                  = 0x3b
	DEVLINK_ATTR_DPIPE_FIELD_MAPPING_TYPE              = 0x3c
	DEVLINK_ATTR_PAD                                   = 0x3d
	DEVLINK_ATTR_ESWITCH_ENCAP_MODE                    = 0x3e
	DEVLINK_ATTR_RESOURCE_LIST                         = 0x3f
	DEVLINK_ATTR_RESOURCE                              = 0x40
	DEVLINK_ATTR_RESOURCE_NAME                         = 0x41
	DEVLINK_ATTR_RESOURCE_ID                           = 0x42
	DEVLINK_ATTR_RESOURCE_SIZE                         = 0x43
	DEVLINK_ATTR_RESOURCE_SIZE_NEW                     = 0x44
	DEVLINK_ATTR_RESOURCE_SIZE_VALID                   = 0x45
	DEVLINK_ATTR_RESOURCE_SIZE_MIN                     = 0x46
	DEVLINK_ATTR_RESOURCE_SIZE_MAX                     = 0x47
	DEVLINK_ATTR_RESOURCE_SIZE_GRAN                    = 0x48
	DEVLINK_ATTR_RESOURCE_UNIT                         = 0x49
	DEVLINK_ATTR_RESOURCE_OCC                          = 0x4a
	DEVLINK_ATTR_DPIPE_TABLE_RESOURCE_ID               = 0x4b
	DEVLINK_ATTR_DPIPE_TABLE_RESOURCE_UNITS            = 0x4c
	DEVLINK_ATTR_PORT_FLAVOUR                          = 0x4d
	DEVLINK_ATTR_PORT_NUMBER                           = 0x4e
	DEVLINK_ATTR_PORT_SPLIT_SUBPORT_NUMBER             = 0x4f
	DEVLINK_ATTR_PARAM                                 = 0x50
	DEVLINK_ATTR_PARAM_NAME                            = 0x51
	DEVLINK_ATTR_PARAM_GENERIC                         = 0x52
	DEVLINK_ATTR_PARAM_TYPE                            = 0x53
	DEVLINK_ATTR_PARAM_VALUES_LIST                     = 0x54
	DEVLINK_ATTR_PARAM_VALUE                           = 0x55
	DEVLINK_ATTR_PARAM_VALUE_DATA                      = 0x56
	DEVLINK_ATTR_PARAM_VALUE_CMODE                     = 0x57
	DEVLINK_ATTR_REGION_NAME                           = 0x58
	DEVLINK_ATTR_REGION_SIZE                           = 0x59
	DEVLINK_ATTR_REGION_SNAPSHOTS                      = 0x5a
	DEVLINK_ATTR_REGION_SNAPSHOT                       = 0x5b
	DEVLINK_ATTR_REGION_SNAPSHOT_ID                    = 0x5c
	DEVLINK_ATTR_REGION_CHUNKS                         = 0x5d
	DEVLINK_ATTR_REGION_CHUNK                          = 0x5e
	DEVLINK_ATTR_REGION_CHUNK_DATA                     = 0x5f
	DEVLINK_ATTR_REGION_CHUNK_ADDR                     = 0x60
	DEVLINK_ATTR_REGION_CHUNK_LEN                      = 0x61
	DEVLINK_ATTR_INFO_DRIVER_NAME                      = 0x62
	DEVLINK_ATTR_INFO_SERIAL_NUMBER                    = 0x63
	DEVLINK_ATTR_INFO_VERSION_FIXED                    = 0x64
	DEVLINK_ATTR_INFO_VERSION_RUNNING                  = 0x65
	DEVLINK_ATTR_INFO_VERSION_STORED                   = 0x66
	DEVLINK_ATTR_INFO_VERSION_NAME                     = 0x67
	DEVLINK_ATTR_INFO_VERSION_VALUE                    = 0x68
	DEVLINK_ATTR_SB_POOL_CELL_SIZE                     = 0x69
	DEVLINK_ATTR_FMSG                                  = 0x6a
	DEVLINK_ATTR_FMSG_OBJ_NEST_START                   = 0x6b
	DEVLINK_ATTR_FMSG_PAIR_NEST_START                  = 0x6c
	DEVLINK_ATTR_FMSG_ARR_NEST_START                   = 0x6d
	DEVLINK_ATTR_FMSG_NEST_END                         = 0x6e
	DEVLINK_ATTR_FMSG_OBJ_NAME                         = 0x6f
	DEVLINK_ATTR_FMSG_OBJ_VALUE_TYPE                   = 0x70
	DEVLINK_ATTR_FMSG_OBJ_VALUE_DATA                   = 0x71
	DEVLINK_ATTR_HEALTH_REPORTER                       = 0x72
	DEVLINK_ATTR_HEALTH_REPORTER_NAME                  = 0x73
	DEVLINK_ATTR_HEALTH_REPORTER_STATE                 = 0x74
	DEVLINK_ATTR_HEALTH_REPORTER_ERR_COUNT             = 0x75
	DEVLINK_ATTR_HEALTH_REPORTER_RECOVER_COUNT         = 0x76
	DEVLINK_ATTR_HEALTH_REPORTER_DUMP_TS               = 0x77
	DEVLINK_ATTR_HEALTH_REPORTER_GRACEFUL_PERIOD       = 0x78
	DEVLINK_ATTR_HEALTH_REPORTER_AUTO_RECOVER          = 0x79
	DEVLINK_ATTR_FLASH_UPDATE_FILE_NAME                = 0x7a
	DEVLINK_ATTR_FLASH_UPDATE_COMPONENT                = 0x7b
	DEVLINK_ATTR_FLASH_UPDATE_STATUS_MSG               = 0x7c
	DEVLINK_ATTR_FLASH_UPDATE_STATUS_DONE              = 0x7d
	DEVLINK_ATTR_FLASH_UPDATE_STATUS_TOTAL             = 0x7e
	DEVLINK_ATTR_PORT_PCI_PF_NUMBER                    = 0x7f
	DEVLINK_ATTR_PORT_PCI_VF_NUMBER                    = 0x80
	DEVLINK_ATTR_STATS                                 = 0x81
	DEVLINK_ATTR_TRAP_NAME                             = 0x82
	DEVLINK_ATTR_TRAP_ACTION                           = 0x83
	DEVLINK_ATTR_TRAP_TYPE                             = 0x84
	DEVLINK_ATTR_TRAP_GENERIC                          = 0x85
	DEVLINK_ATTR_TRAP_METADATA                         = 0x86
	DEVLINK_ATTR_TRAP_GROUP_NAME                       = 0x87
	DEVLINK_ATTR_RELOAD_FAILED                         = 0x88
	DEVLINK_ATTR_HEALTH_REPORTER_DUMP_TS_NS            = 0x89
	DEVLINK_ATTR_NETNS_FD                              = 0x8a
	DEVLINK_ATTR_NETNS_PID                             = 0x8b
	DEVLINK_ATTR_NETNS_ID                              = 0x8c
	DEVLINK_ATTR_HEALTH_REPORTER_AUTO_DUMP             = 0x8d
	DEVLINK_ATTR_TRAP_POLICER_ID                       = 0x8e
	DEVLINK_ATTR_TRAP_POLICER_RATE                     = 0x8f
	DEVLINK_ATTR_TRAP_POLICER_BURST                    = 0x90
	DEVLINK_ATTR_PORT_FUNCTION                         = 0x91
	DEVLINK_ATTR_INFO_BOARD_SERIAL_NUMBER              = 0x92
	DEVLINK_ATTR_PORT_LANES                            = 0x93
	DEVLINK_ATTR_PORT_SPLITTABLE                       = 0x94
	DEVLINK_ATTR_PORT_EXTERNAL                         = 0x95
	DEVLINK_ATTR_PORT_CONTROLLER_NUMBER                = 0x96
	DEVLINK_ATTR_FLASH_UPDATE_STATUS_TIMEOUT           = 0x97
	DEVLINK_ATTR_FLASH_UPDATE_OVERWRITE_MASK           = 0x98
	DEVLINK_ATTR_RELOAD_ACTION                         = 0x99
	DEVLINK_ATTR_RELOAD_ACTIONS_PERFORMED              = 0x9a
	DEVLINK_ATTR_RELOAD_LIMITS                         = 0x9b
	DEVLINK_ATTR_DEV_STATS                             = 0x9c
	DEVLINK_ATTR_RELOAD_STATS                          = 0x9d
	DEVLINK_ATTR_RELOAD_STATS_ENTRY                    = 0x9e
	DEVLINK_ATTR_RELOAD_STATS_LIMIT                    = 0x9f
	DEVLINK_ATTR_RELOAD_STATS_VALUE                    = 0xa0
	DEVLINK_ATTR_REMOTE_RELOAD_STATS                   = 0xa1
	DEVLINK_ATTR_RELOAD_ACTION_INFO                    = 0xa2
	DEVLINK_ATTR_RELOAD_ACTION_STATS                   = 0xa3
	DEVLINK_ATTR_PORT_PCI_SF_NUMBER                    = 0xa4
	DEVLINK_ATTR_RATE_TYPE                             = 0xa5
	DEVLINK_ATTR_RATE_TX_SHARE                         = 0xa6
	DEVLINK_ATTR_RATE_TX_MAX                           = 0xa7
	DEVLINK_ATTR_RATE_NODE_NAME                        = 0xa8
	DEVLINK_ATTR_RATE_PARENT_NODE_NAME                 = 0xa9
	DEVLINK_ATTR_REGION_MAX_SNAPSHOTS                  = 0xaa
	DEVLINK_ATTR_LINECARD_INDEX                        = 0xab
	DEVLINK_ATTR_LINECARD_STATE                        = 0xac
	DEVLINK_ATTR_LINECARD_TYPE                         = 0xad
	DEVLINK_ATTR_LINECARD_SUPPORTED_TYPES              = 0xae
	DEVLINK_ATTR_NESTED_DEVLINK                        = 0xaf
	DEVLINK_ATTR_SELFTESTS                             = 0xb0
	DEVLINK_ATTR_MAX                                   = 0xb3
	DEVLINK_DPIPE_FIELD_MAPPING_TYPE_NONE              = 0x0
	DEVLINK_DPIPE_FIELD_MAPPING_TYPE_IFINDEX           = 0x1
	DEVLINK_DPIPE_MATCH_TYPE_FIELD_EXACT               = 0x0
	DEVLINK_DPIPE_ACTION_TYPE_FIELD_MODIFY             = 0x0
	DEVLINK_DPIPE_FIELD_ETHERNET_DST_MAC               = 0x0
	DEVLINK_DPIPE_FIELD_IPV4_DST_IP                    = 0x0
	DEVLINK_DPIPE_FIELD_IPV6_DST_IP                    = 0x0
	DEVLINK_DPIPE_HEADER_ETHERNET                      = 0x0
	DEVLINK_DPIPE_HEADER_IPV4                          = 0x1
	DEVLINK_DPIPE_HEADER_IPV6                          = 0x2
	DEVLINK_RESOURCE_UNIT_ENTRY                        = 0x0
	DEVLINK_PORT_FUNCTION_ATTR_UNSPEC                  = 0x0
	DEVLINK_PORT_FUNCTION_ATTR_HW_ADDR                 = 0x1
	DEVLINK_PORT_FN_ATTR_STATE                         = 0x2
	DEVLINK_PORT_FN_ATTR_OPSTATE                       = 0x3
	DEVLINK_PORT_FN_ATTR_CAPS                          = 0x4
	DEVLINK_PORT_FUNCTION_ATTR_MAX                     = 0x6
)

type FsverityDigest struct {
	Algorithm uint16
	Size      uint16
}

type FsverityEnableArg struct {
	Version        uint32
	Hash_algorithm uint32
	Block_size     uint32
	Salt_size      uint32
	Salt_ptr       uint64
	Sig_size       uint32
	_              uint32
	Sig_ptr        uint64
	_              [11]uint64
}

type Nhmsg struct {
	Family   uint8
	Scope    uint8
	Protocol uint8
	Resvd    uint8
	Flags    uint32
}

type NexthopGrp struct {
	Id     uint32
	Weight uint8
	High   uint8
	Resvd2 uint16
}

const (
	NHA_UNSPEC     = 0x0
	NHA_ID         = 0x1
	NHA_GROUP      = 0x2
	NHA_GROUP_TYPE = 0x3
	NHA_BLACKHOLE  = 0x4
	NHA_OIF        = 0x5
	NHA_GATEWAY    = 0x6
	NHA_ENCAP_TYPE = 0x7
	NHA_ENCAP      = 0x8
	NHA_GROUPS     = 0x9
	NHA_MASTER     = 0xa
)

const (
	CAN_RAW_FILTER        = 0x1
	CAN_RAW_ERR_FILTER    = 0x2
	CAN_RAW_LOOPBACK      = 0x3
	CAN_RAW_RECV_OWN_MSGS = 0x4
	CAN_RAW_FD_FRAMES     = 0x5
	CAN_RAW_JOIN_FILTERS  = 0x6
)

type WatchdogInfo struct {
	Options  uint32
	Version  uint32
	Identity [32]uint8
}

type PPSFData struct {
	Info    PPSKInfo
	Timeout PPSKTime
}

type PPSKParams struct {
	Api_version   int32
	Mode          int32
	Assert_off_tu PPSKTime
	Clear_off_tu  PPSKTime
}

type PPSKTime struct {
	Sec   int64
	Nsec  int32
	Flags uint32
}

const (
	LWTUNNEL_ENCAP_NONE       = 0x0
	LWTUNNEL_ENCAP_MPLS       = 0x1
	LWTUNNEL_ENCAP_IP         = 0x2
	LWTUNNEL_ENCAP_ILA        = 0x3
	LWTUNNEL_ENCAP_IP6        = 0x4
	LWTUNNEL_ENCAP_SEG6       = 0x5
	LWTUNNEL_ENCAP_BPF        = 0x6
	LWTUNNEL_ENCAP_SEG6_LOCAL = 0x7
	LWTUNNEL_ENCAP_RPL        = 0x8
	LWTUNNEL_ENCAP_IOAM6      = 0x9
	LWTUNNEL_ENCAP_XFRM       = 0xa
	LWTUNNEL_ENCAP_MAX        = 0xa

	MPLS_IPTUNNEL_UNSPEC = 0x0
	MPLS_IPTUNNEL_DST    = 0x1
	MPLS_IPTUNNEL_TTL    = 0x2
	MPLS_IPTUNNEL_MAX    = 0x2
)

const (
	ETHTOOL_ID_UNSPEC                                                       = 0x0
	ETHTOOL_RX_COPYBREAK                                                    = 0x1
	ETHTOOL_TX_COPYBREAK                                                    = 0x2
	ETHTOOL_PFC_PREVENTION_TOUT                                             = 0x3
	ETHTOOL_TUNABLE_UNSPEC                                                  = 0x0
	ETHTOOL_TUNABLE_U8                                                      = 0x1
	ETHTOOL_TUNABLE_U16                                                     = 0x2
	ETHTOOL_TUNABLE_U32                                                     = 0x3
	ETHTOOL_TUNABLE_U64                                                     = 0x4
	ETHTOOL_TUNABLE_STRING                                                  = 0x5
	ETHTOOL_TUNABLE_S8                                                      = 0x6
	ETHTOOL_TUNABLE_S16                                                     = 0x7
	ETHTOOL_TUNABLE_S32                                                     = 0x8
	ETHTOOL_TUNABLE_S64                                                     = 0x9
	ETHTOOL_PHY_ID_UNSPEC                                                   = 0x0
	ETHTOOL_PHY_DOWNSHIFT                                                   = 0x1
	ETHTOOL_PHY_FAST_LINK_DOWN                                              = 0x2
	ETHTOOL_PHY_EDPD                                                        = 0x3
	ETHTOOL_LINK_EXT_STATE_AUTONEG                                          = 0x0
	ETHTOOL_LINK_EXT_STATE_LINK_TRAINING_FAILURE                            = 0x1
	ETHTOOL_LINK_EXT_STATE_LINK_LOGICAL_MISMATCH                            = 0x2
	ETHTOOL_LINK_EXT_STATE_BAD_SIGNAL_INTEGRITY                             = 0x3
	ETHTOOL_LINK_EXT_STATE_NO_CABLE                                         = 0x4
	ETHTOOL_LINK_EXT_STATE_CABLE_ISSUE                                      = 0x5
	ETHTOOL_LINK_EXT_STATE_EEPROM_ISSUE                                     = 0x6
	ETHTOOL_LINK_EXT_STATE_CALIBRATION_FAILURE                              = 0x7
	ETHTOOL_LINK_EXT_STATE_POWER_BUDGET_EXCEEDED                            = 0x8
	ETHTOOL_LINK_EXT_STATE_OVERHEAT                                         = 0x9
	ETHTOOL_LINK_EXT_SUBSTATE_AN_NO_PARTNER_DETECTED                        = 0x1
	ETHTOOL_LINK_EXT_SUBSTATE_AN_ACK_NOT_RECEIVED                           = 0x2
	ETHTOOL_LINK_EXT_SUBSTATE_AN_NEXT_PAGE_EXCHANGE_FAILED                  = 0x3
	ETHTOOL_LINK_EXT_SUBSTATE_AN_NO_PARTNER_DETECTED_FORCE_MODE             = 0x4
	ETHTOOL_LINK_EXT_SUBSTATE_AN_FEC_MISMATCH_DURING_OVERRIDE               = 0x5
	ETHTOOL_LINK_EXT_SUBSTATE_AN_NO_HCD                                     = 0x6
	ETHTOOL_LINK_EXT_SUBSTATE_LT_KR_FRAME_LOCK_NOT_ACQUIRED                 = 0x1
	ETHTOOL_LINK_EXT_SUBSTATE_LT_KR_LINK_INHIBIT_TIMEOUT                    = 0x2
	ETHTOOL_LINK_EXT_SUBSTATE_LT_KR_LINK_PARTNER_DID_NOT_SET_RECEIVER_READY = 0x3
	ETHTOOL_LINK_EXT_SUBSTATE_LT_REMOTE_FAULT                               = 0x4
	ETHTOOL_LINK_EXT_SUBSTATE_LLM_PCS_DID_NOT_ACQUIRE_BLOCK_LOCK            = 0x1
	ETHTOOL_LINK_EXT_SUBSTATE_LLM_PCS_DID_NOT_ACQUIRE_AM_LOCK               = 0x2
	ETHTOOL_LINK_EXT_SUBSTATE_LLM_PCS_DID_NOT_GET_ALIGN_STATUS              = 0x3
	ETHTOOL_LINK_EXT_SUBSTATE_LLM_FC_FEC_IS_NOT_LOCKED                      = 0x4
	ETHTOOL_LINK_EXT_SUBSTATE_LLM_RS_FEC_IS_NOT_LOCKED                      = 0x5
	ETHTOOL_LINK_EXT_SUBSTATE_BSI_LARGE_NUMBER_OF_PHYSICAL_ERRORS           = 0x1
	ETHTOOL_LINK_EXT_SUBSTATE_BSI_UNSUPPORTED_RATE                          = 0x2
	ETHTOOL_LINK_EXT_SUBSTATE_CI_UNSUPPORTED_CABLE                          = 0x1
	ETHTOOL_LINK_EXT_SUBSTATE_CI_CABLE_TEST_FAILURE                         = 0x2
	ETHTOOL_FLASH_ALL_REGIONS                                               = 0x0
	ETHTOOL_F_UNSUPPORTED__BIT                                              = 0x0
	ETHTOOL_F_WISH__BIT                                                     = 0x1
	ETHTOOL_F_COMPAT__BIT                                                   = 0x2
	ETHTOOL_FEC_NONE_BIT                                                    = 0x0
	ETHTOOL_FEC_AUTO_BIT                                                    = 0x1
	ETHTOOL_FEC_OFF_BIT                                                     = 0x2
	ETHTOOL_FEC_RS_BIT                                                      = 0x3
	ETHTOOL_FEC_BASER_BIT                                                   = 0x4
	ETHTOOL_FEC_LLRS_BIT                                                    = 0x5
	ETHTOOL_LINK_MODE_10baseT_Half_BIT                                      = 0x0
	ETHTOOL_LINK_MODE_10baseT_Full_BIT                                      = 0x1
	ETHTOOL_LINK_MODE_100baseT_Half_BIT                                     = 0x2
	ETHTOOL_LINK_MODE_100baseT_Full_BIT                                     = 0x3
	ETHTOOL_LINK_MODE_1000baseT_Half_BIT                                    = 0x4
	ETHTOOL_LINK_MODE_1000baseT_Full_BIT                                    = 0x5
	ETHTOOL_LINK_MODE_Autoneg_BIT                                           = 0x6
	ETHTOOL_LINK_MODE_TP_BIT                                                = 0x7
	ETHTOOL_LINK_MODE_AUI_BIT                                               = 0x8
	ETHTOOL_LINK_MODE_MII_BIT                                               = 0x9
	ETHTOOL_LINK_MODE_FIBRE_BIT                                             = 0xa
	ETHTOOL_LINK_MODE_BNC_BIT                                               = 0xb
	ETHTOOL_LINK_MODE_10000baseT_Full_BIT                                   = 0xc
	ETHTOOL_LINK_MODE_Pause_BIT                                             = 0xd
	ETHTOOL_LINK_MODE_Asym_Pause_BIT                                        = 0xe
	ETHTOOL_LINK_MODE_2500baseX_Full_BIT                                    = 0xf
	ETHTOOL_LINK_MODE_Backplane_BIT                                         = 0x10
	ETHTOOL_LINK_MODE_1000baseKX_Full_BIT                                   = 0x11
	ETHTOOL_LINK_MODE_10000baseKX4_Full_BIT                                 = 0x12
	ETHTOOL_LINK_MODE_10000baseKR_Full_BIT                                  = 0x13
	ETHTOOL_LINK_MODE_10000baseR_FEC_BIT                                    = 0x14
	ETHTOOL_LINK_MODE_20000baseMLD2_Full_BIT                                = 0x15
	ETHTOOL_LINK_MODE_20000baseKR2_Full_BIT                                 = 0x16
	ETHTOOL_LINK_MODE_40000baseKR4_Full_BIT                                 = 0x17
	ETHTOOL_LINK_MODE_40000baseCR4_Full_BIT                                 = 0x18
	ETHTOOL_LINK_MODE_40000baseSR4_Full_BIT                                 = 0x19
	ETHTOOL_LINK_MODE_40000baseLR4_Full_BIT                                 = 0x1a
	ETHTOOL_LINK_MODE_56000baseKR4_Full_BIT                                 = 0x1b
	ETHTOOL_LINK_MODE_56000baseCR4_Full_BIT                                 = 0x1c
	ETHTOOL_LINK_MODE_56000baseSR4_Full_BIT                                 = 0x1d
	ETHTOOL_LINK_MODE_56000baseLR4_Full_BIT                                 = 0x1e
	ETHTOOL_LINK_MODE_25000baseCR_Full_BIT                                  = 0x1f
	ETHTOOL_LINK_MODE_25000baseKR_Full_BIT                                  = 0x20
	ETHTOOL_LINK_MODE_25000baseSR_Full_BIT                                  = 0x21
	ETHTOOL_LINK_MODE_50000baseCR2_Full_BIT                                 = 0x22
	ETHTOOL_LINK_MODE_50000baseKR2_Full_BIT                                 = 0x23
	ETHTOOL_LINK_MODE_100000baseKR4_Full_BIT                                = 0x24
	ETHTOOL_LINK_MODE_100000baseSR4_Full_BIT                                = 0x25
	ETHTOOL_LINK_MODE_100000baseCR4_Full_BIT                                = 0x26
	ETHTOOL_LINK_MODE_100000baseLR4_ER4_Full_BIT                            = 0x27
	ETHTOOL_LINK_MODE_50000baseSR2_Full_BIT                                 = 0x28
	ETHTOOL_LINK_MODE_1000baseX_Full_BIT                                    = 0x29
	ETHTOOL_LINK_MODE_10000baseCR_Full_BIT                                  = 0x2a
	ETHTOOL_LINK_MODE_10000baseSR_Full_BIT                                  = 0x2b
	ETHTOOL_LINK_MODE_10000baseLR_Full_BIT                                  = 0x2c
	ETHTOOL_LINK_MODE_10000baseLRM_Full_BIT                                 = 0x2d
	ETHTOOL_LINK_MODE_10000baseER_Full_BIT                                  = 0x2e
	ETHTOOL_LINK_MODE_2500baseT_Full_BIT                                    = 0x2f
	ETHTOOL_LINK_MODE_5000baseT_Full_BIT                                    = 0x30
	ETHTOOL_LINK_MODE_FEC_NONE_BIT                                          = 0x31
	ETHTOOL_LINK_MODE_FEC_RS_BIT                                            = 0x32
	ETHTOOL_LINK_MODE_FEC_BASER_BIT                                         = 0x33
	ETHTOOL_LINK_MODE_50000baseKR_Full_BIT                                  = 0x34
	ETHTOOL_LINK_MODE_50000baseSR_Full_BIT                                  = 0x35
	ETHTOOL_LINK_MODE_50000baseCR_Full_BIT                                  = 0x36
	ETHTOOL_LINK_MODE_50000baseLR_ER_FR_Full_BIT                            = 0x37
	ETHTOOL_LINK_MODE_50000baseDR_Full_BIT                                  = 0x38
	ETHTOOL_LINK_MODE_100000baseKR2_Full_BIT                                = 0x39
	ETHTOOL_LINK_MODE_100000baseSR2_Full_BIT                                = 0x3a
	ETHTOOL_LINK_MODE_100000baseCR2_Full_BIT                                = 0x3b
	ETHTOOL_LINK_MODE_100000baseLR2_ER2_FR2_Full_BIT                        = 0x3c
	ETHTOOL_LINK_MODE_100000baseDR2_Full_BIT                                = 0x3d
	ETHTOOL_LINK_MODE_200000baseKR4_Full_BIT                                = 0x3e
	ETHTOOL_LINK_MODE_200000baseSR4_Full_BIT                                = 0x3f
	ETHTOOL_LINK_MODE_200000baseLR4_ER4_FR4_Full_BIT                        = 0x40
	ETHTOOL_LINK_MODE_200000baseDR4_Full_BIT                                = 0x41
	ETHTOOL_LINK_MODE_200000baseCR4_Full_BIT                                = 0x42
	ETHTOOL_LINK_MODE_100baseT1_Full_BIT                                    = 0x43
	ETHTOOL_LINK_MODE_1000baseT1_Full_BIT                                   = 0x44
	ETHTOOL_LINK_MODE_400000baseKR8_Full_BIT                                = 0x45
	ETHTOOL_LINK_MODE_400000baseSR8_Full_BIT                                = 0x46
	ETHTOOL_LINK_MODE_400000baseLR8_ER8_FR8_Full_BIT                        = 0x47
	ETHTOOL_LINK_MODE_400000baseDR8_Full_BIT                                = 0x48
	ETHTOOL_LINK_MODE_400000baseCR8_Full_BIT                                = 0x49
	ETHTOOL_LINK_MODE_FEC_LLRS_BIT                                          = 0x4a
	ETHTOOL_LINK_MODE_100000baseKR_Full_BIT                                 = 0x4b
	ETHTOOL_LINK_MODE_100000baseSR_Full_BIT                                 = 0x4c
	ETHTOOL_LINK_MODE_100000baseLR_ER_FR_Full_BIT                           = 0x4d
	ETHTOOL_LINK_MODE_100000baseCR_Full_BIT                                 = 0x4e
	ETHTOOL_LINK_MODE_100000baseDR_Full_BIT                                 = 0x4f
	ETHTOOL_LINK_MODE_200000baseKR2_Full_BIT                                = 0x50
	ETHTOOL_LINK_MODE_200000baseSR2_Full_BIT                                = 0x51
	ETHTOOL_LINK_MODE_200000baseLR2_ER2_FR2_Full_BIT                        = 0x52
	ETHTOOL_LINK_MODE_200000baseDR2_Full_BIT                                = 0x53
	ETHTOOL_LINK_MODE_200000baseCR2_Full_BIT                                = 0x54
	ETHTOOL_LINK_MODE_400000baseKR4_Full_BIT                                = 0x55
	ETHTOOL_LINK_MODE_400000baseSR4_Full_BIT                                = 0x56
	ETHTOOL_LINK_MODE_400000baseLR4_ER4_FR4_Full_BIT                        = 0x57
	ETHTOOL_LINK_MODE_400000baseDR4_Full_BIT                                = 0x58
	ETHTOOL_LINK_MODE_400000baseCR4_Full_BIT                                = 0x59
	ETHTOOL_LINK_MODE_100baseFX_Half_BIT                                    = 0x5a
	ETHTOOL_LINK_MODE_100baseFX_Full_BIT                                    = 0x5b

	ETHTOOL_MSG_USER_NONE                     = 0x0
	ETHTOOL_MSG_STRSET_GET                    = 0x1
	ETHTOOL_MSG_LINKINFO_GET                  = 0x2
	ETHTOOL_MSG_LINKINFO_SET                  = 0x3
	ETHTOOL_MSG_LINKMODES_GET                 = 0x4
	ETHTOOL_MSG_LINKMODES_SET                 = 0x5
	ETHTOOL_MSG_LINKSTATE_GET                 = 0x6
	ETHTOOL_MSG_DEBUG_GET                     = 0x7
	ETHTOOL_MSG_DEBUG_SET                     = 0x8
	ETHTOOL_MSG_WOL_GET                       = 0x9
	ETHTOOL_MSG_WOL_SET                       = 0xa
	ETHTOOL_MSG_FEATURES_GET                  = 0xb
	ETHTOOL_MSG_FEATURES_SET                  = 0xc
	ETHTOOL_MSG_PRIVFLAGS_GET                 = 0xd
	ETHTOOL_MSG_PRIVFLAGS_SET                 = 0xe
	ETHTOOL_MSG_RINGS_GET                     = 0xf
	ETHTOOL_MSG_RINGS_SET                     = 0x10
	ETHTOOL_MSG_CHANNELS_GET                  = 0x11
	ETHTOOL_MSG_CHANNELS_SET                  = 0x12
	ETHTOOL_MSG_COALESCE_GET                  = 0x13
	ETHTOOL_MSG_COALESCE_SET                  = 0x14
	ETHTOOL_MSG_PAUSE_GET                     = 0x15
	ETHTOOL_MSG_PAUSE_SET                     = 0x16
	ETHTOOL_MSG_EEE_GET                       = 0x17
	ETHTOOL_MSG_EEE_SET                       = 0x18
	ETHTOOL_MSG_TSINFO_GET                    = 0x19
	ETHTOOL_MSG_CABLE_TEST_ACT                = 0x1a
	ETHTOOL_MSG_CABLE_TEST_TDR_ACT            = 0x1b
	ETHTOOL_MSG_TUNNEL_INFO_GET               = 0x1c
	ETHTOOL_MSG_FEC_GET                       = 0x1d
	ETHTOOL_MSG_FEC_SET                       = 0x1e
	ETHTOOL_MSG_MODULE_EEPROM_GET             = 0x1f
	ETHTOOL_MSG_STATS_GET                     = 0x20
	ETHTOOL_MSG_PHC_VCLOCKS_GET               = 0x21
	ETHTOOL_MSG_MODULE_GET                    = 0x22
	ETHTOOL_MSG_MODULE_SET                    = 0x23
	ETHTOOL_MSG_PSE_GET                       = 0x24
	ETHTOOL_MSG_PSE_SET                       = 0x25
	ETHTOOL_MSG_RSS_GET                       = 0x26
	ETHTOOL_MSG_PLCA_GET_CFG                  = 0x27
	ETHTOOL_MSG_PLCA_SET_CFG                  = 0x28
	ETHTOOL_MSG_PLCA_GET_STATUS               = 0x29
	ETHTOOL_MSG_MM_GET                        = 0x2a
	ETHTOOL_MSG_MM_SET                        = 0x2b
	ETHTOOL_MSG_MODULE_FW_FLASH_ACT           = 0x2c
	ETHTOOL_MSG_PHY_GET                       = 0x2d
	ETHTOOL_MSG_TSCONFIG_GET                  = 0x2e
	ETHTOOL_MSG_TSCONFIG_SET                  = 0x2f
	ETHTOOL_MSG_USER_MAX                      = 0x2f
	ETHTOOL_MSG_KERNEL_NONE                   = 0x0
	ETHTOOL_MSG_STRSET_GET_REPLY              = 0x1
	ETHTOOL_MSG_LINKINFO_GET_REPLY            = 0x2
	ETHTOOL_MSG_LINKINFO_NTF                  = 0x3
	ETHTOOL_MSG_LINKMODES_GET_REPLY           = 0x4
	ETHTOOL_MSG_LINKMODES_NTF                 = 0x5
	ETHTOOL_MSG_LINKSTATE_GET_REPLY           = 0x6
	ETHTOOL_MSG_DEBUG_GET_REPLY               = 0x7
	ETHTOOL_MSG_DEBUG_NTF                     = 0x8
	ETHTOOL_MSG_WOL_GET_REPLY                 = 0x9
	ETHTOOL_MSG_WOL_NTF                       = 0xa
	ETHTOOL_MSG_FEATURES_GET_REPLY            = 0xb
	ETHTOOL_MSG_FEATURES_SET_REPLY            = 0xc
	ETHTOOL_MSG_FEATURES_NTF                  = 0xd
	ETHTOOL_MSG_PRIVFLAGS_GET_REPLY           = 0xe
	ETHTOOL_MSG_PRIVFLAGS_NTF                 = 0xf
	ETHTOOL_MSG_RINGS_GET_REPLY               = 0x10
	ETHTOOL_MSG_RINGS_NTF                     = 0x11
	ETHTOOL_MSG_CHANNELS_GET_REPLY            = 0x12
	ETHTOOL_MSG_CHANNELS_NTF                  = 0x13
	ETHTOOL_MSG_COALESCE_GET_REPLY            = 0x14
	ETHTOOL_MSG_COALESCE_NTF                  = 0x15
	ETHTOOL_MSG_PAUSE_GET_REPLY               = 0x16
	ETHTOOL_MSG_PAUSE_NTF                     = 0x17
	ETHTOOL_MSG_EEE_GET_REPLY                 = 0x18
	ETHTOOL_MSG_EEE_NTF                       = 0x19
	ETHTOOL_MSG_TSINFO_GET_REPLY              = 0x1a
	ETHTOOL_MSG_CABLE_TEST_NTF                = 0x1b
	ETHTOOL_MSG_CABLE_TEST_TDR_NTF            = 0x1c
	ETHTOOL_MSG_TUNNEL_INFO_GET_REPLY         = 0x1d
	ETHTOOL_MSG_FEC_GET_REPLY                 = 0x1e
	ETHTOOL_MSG_FEC_NTF                       = 0x1f
	ETHTOOL_MSG_MODULE_EEPROM_GET_REPLY       = 0x20
	ETHTOOL_MSG_STATS_GET_REPLY               = 0x21
	ETHTOOL_MSG_PHC_VCLOCKS_GET_REPLY         = 0x22
	ETHTOOL_MSG_MODULE_GET_REPLY              = 0x23
	ETHTOOL_MSG_MODULE_NTF                    = 0x24
	ETHTOOL_MSG_PSE_GET_REPLY                 = 0x25
	ETHTOOL_MSG_RSS_GET_REPLY                 = 0x26
	ETHTOOL_MSG_PLCA_GET_CFG_REPLY            = 0x27
	ETHTOOL_MSG_PLCA_GET_STATUS_REPLY         = 0x28
	ETHTOOL_MSG_PLCA_NTF                      = 0x29
	ETHTOOL_MSG_MM_GET_REPLY                  = 0x2a
	ETHTOOL_MSG_MM_NTF                        = 0x2b
	ETHTOOL_MSG_MODULE_FW_FLASH_NTF           = 0x2c
	ETHTOOL_MSG_PHY_GET_REPLY                 = 0x2d
	ETHTOOL_MSG_PHY_NTF                       = 0x2e
	ETHTOOL_MSG_TSCONFIG_GET_REPLY            = 0x2f
	ETHTOOL_MSG_TSCONFIG_SET_REPLY            = 0x30
	ETHTOOL_MSG_KERNEL_MAX                    = 0x30
	ETHTOOL_FLAG_COMPACT_BITSETS              = 0x1
	ETHTOOL_FLAG_OMIT_REPLY                   = 0x2
	ETHTOOL_FLAG_STATS                        = 0x4
	ETHTOOL_A_HEADER_UNSPEC                   = 0x0
	ETHTOOL_A_HEADER_DEV_INDEX                = 0x1
	ETHTOOL_A_HEADER_DEV_NAME                 = 0x2
	ETHTOOL_A_HEADER_FLAGS                    = 0x3
	ETHTOOL_A_HEADER_MAX                      = 0x4
	ETHTOOL_A_BITSET_BIT_UNSPEC               = 0x0
	ETHTOOL_A_BITSET_BIT_INDEX                = 0x1
	ETHTOOL_A_BITSET_BIT_NAME                 = 0x2
	ETHTOOL_A_BITSET_BIT_VALUE                = 0x3
	ETHTOOL_A_BITSET_BIT_MAX                  = 0x3
	ETHTOOL_A_BITSET_BITS_UNSPEC              = 0x0
	ETHTOOL_A_BITSET_BITS_BIT                 = 0x1
	ETHTOOL_A_BITSET_BITS_MAX                 = 0x1
	ETHTOOL_A_BITSET_UNSPEC                   = 0x0
	ETHTOOL_A_BITSET_NOMASK                   = 0x1
	ETHTOOL_A_BITSET_SIZE                     = 0x2
	ETHTOOL_A_BITSET_BITS                     = 0x3
	ETHTOOL_A_BITSET_VALUE                    = 0x4
	ETHTOOL_A_BITSET_MASK                     = 0x5
	ETHTOOL_A_BITSET_MAX                      = 0x5
	ETHTOOL_A_STRING_UNSPEC                   = 0x0
	ETHTOOL_A_STRING_INDEX                    = 0x1
	ETHTOOL_A_STRING_VALUE                    = 0x2
	ETHTOOL_A_STRING_MAX                      = 0x2
	ETHTOOL_A_STRINGS_UNSPEC                  = 0x0
	ETHTOOL_A_STRINGS_STRING                  = 0x1
	ETHTOOL_A_STRINGS_MAX                     = 0x1
	ETHTOOL_A_STRINGSET_UNSPEC                = 0x0
	ETHTOOL_A_STRINGSET_ID                    = 0x1
	ETHTOOL_A_STRINGSET_COUNT                 = 0x2
	ETHTOOL_A_STRINGSET_STRINGS               = 0x3
	ETHTOOL_A_STRINGSET_MAX                   = 0x3
	ETHTOOL_A_STRINGSETS_UNSPEC               = 0x0
	ETHTOOL_A_STRINGSETS_STRINGSET            = 0x1
	ETHTOOL_A_STRINGSETS_MAX                  = 0x1
	ETHTOOL_A_STRSET_UNSPEC                   = 0x0
	ETHTOOL_A_STRSET_HEADER                   = 0x1
	ETHTOOL_A_STRSET_STRINGSETS               = 0x2
	ETHTOOL_A_STRSET_COUNTS_ONLY              = 0x3
	ETHTOOL_A_STRSET_MAX                      = 0x3
	ETHTOOL_A_LINKINFO_UNSPEC                 = 0x0
	ETHTOOL_A_LINKINFO_HEADER                 = 0x1
	ETHTOOL_A_LINKINFO_PORT                   = 0x2
	ETHTOOL_A_LINKINFO_PHYADDR                = 0x3
	ETHTOOL_A_LINKINFO_TP_MDIX                = 0x4
	ETHTOOL_A_LINKINFO_TP_MDIX_CTRL           = 0x5
	ETHTOOL_A_LINKINFO_TRANSCEIVER            = 0x6
	ETHTOOL_A_LINKINFO_MAX                    = 0x6
	ETHTOOL_A_LINKMODES_UNSPEC                = 0x0
	ETHTOOL_A_LINKMODES_HEADER                = 0x1
	ETHTOOL_A_LINKMODES_AUTONEG               = 0x2
	ETHTOOL_A_LINKMODES_OURS                  = 0x3
	ETHTOOL_A_LINKMODES_PEER                  = 0x4
	ETHTOOL_A_LINKMODES_SPEED                 = 0x5
	ETHTOOL_A_LINKMODES_DUPLEX                = 0x6
	ETHTOOL_A_LINKMODES_MASTER_SLAVE_CFG      = 0x7
	ETHTOOL_A_LINKMODES_MASTER_SLAVE_STATE    = 0x8
	ETHTOOL_A_LINKMODES_LANES                 = 0x9
	ETHTOOL_A_LINKMODES_RATE_MATCHING         = 0xa
	ETHTOOL_A_LINKMODES_MAX                   = 0xa
	ETHTOOL_A_LINKSTATE_UNSPEC                = 0x0
	ETHTOOL_A_LINKSTATE_HEADER                = 0x1
	ETHTOOL_A_LINKSTATE_LINK                  = 0x2
	ETHTOOL_A_LINKSTATE_SQI                   = 0x3
	ETHTOOL_A_LINKSTATE_SQI_MAX               = 0x4
	ETHTOOL_A_LINKSTATE_EXT_STATE             = 0x5
	ETHTOOL_A_LINKSTATE_EXT_SUBSTATE          = 0x6
	ETHTOOL_A_LINKSTATE_EXT_DOWN_CNT          = 0x7
	ETHTOOL_A_LINKSTATE_MAX                   = 0x7
	ETHTOOL_A_DEBUG_UNSPEC                    = 0x0
	ETHTOOL_A_DEBUG_HEADER                    = 0x1
	ETHTOOL_A_DEBUG_MSGMASK                   = 0x2
	ETHTOOL_A_DEBUG_MAX                       = 0x2
	ETHTOOL_A_WOL_UNSPEC                      = 0x0
	ETHTOOL_A_WOL_HEADER                      = 0x1
	ETHTOOL_A_WOL_MODES                       = 0x2
	ETHTOOL_A_WOL_SOPASS                      = 0x3
	ETHTOOL_A_WOL_MAX                         = 0x3
	ETHTOOL_A_FEATURES_UNSPEC                 = 0x0
	ETHTOOL_A_FEATURES_HEADER                 = 0x1
	ETHTOOL_A_FEATURES_HW                     = 0x2
	ETHTOOL_A_FEATURES_WANTED                 = 0x3
	ETHTOOL_A_FEATURES_ACTIVE                 = 0x4
	ETHTOOL_A_FEATURES_NOCHANGE               = 0x5
	ETHTOOL_A_FEATURES_MAX                    = 0x5
	ETHTOOL_A_PRIVFLAGS_UNSPEC                = 0x0
	ETHTOOL_A_PRIVFLAGS_HEADER                = 0x1
	ETHTOOL_A_PRIVFLAGS_FLAGS                 = 0x2
	ETHTOOL_A_PRIVFLAGS_MAX                   = 0x2
	ETHTOOL_A_RINGS_UNSPEC                    = 0x0
	ETHTOOL_A_RINGS_HEADER                    = 0x1
	ETHTOOL_A_RINGS_RX_MAX                    = 0x2
	ETHTOOL_A_RINGS_RX_MINI_MAX               = 0x3
	ETHTOOL_A_RINGS_RX_JUMBO_MAX              = 0x4
	ETHTOOL_A_RINGS_TX_MAX                    = 0x5
	ETHTOOL_A_RINGS_RX                        = 0x6
	ETHTOOL_A_RINGS_RX_MINI                   = 0x7
	ETHTOOL_A_RINGS_RX_JUMBO                  = 0x8
	ETHTOOL_A_RINGS_TX                        = 0x9
	ETHTOOL_A_RINGS_RX_BUF_LEN                = 0xa
	ETHTOOL_A_RINGS_TCP_DATA_SPLIT            = 0xb
	ETHTOOL_A_RINGS_CQE_SIZE                  = 0xc
	ETHTOOL_A_RINGS_TX_PUSH                   = 0xd
	ETHTOOL_A_RINGS_RX_PUSH                   = 0xe
	ETHTOOL_A_RINGS_TX_PUSH_BUF_LEN           = 0xf
	ETHTOOL_A_RINGS_TX_PUSH_BUF_LEN_MAX       = 0x10
	ETHTOOL_A_RINGS_HDS_THRESH                = 0x11
	ETHTOOL_A_RINGS_HDS_THRESH_MAX            = 0x12
	ETHTOOL_A_RINGS_MAX                       = 0x12
	ETHTOOL_A_CHANNELS_UNSPEC                 = 0x0
	ETHTOOL_A_CHANNELS_HEADER                 = 0x1
	ETHTOOL_A_CHANNELS_RX_MAX                 = 0x2
	ETHTOOL_A_CHANNELS_TX_MAX                 = 0x3
	ETHTOOL_A_CHANNELS_OTHER_MAX              = 0x4
	ETHTOOL_A_CHANNELS_COMBINED_MAX           = 0x5
	ETHTOOL_A_CHANNELS_RX_COUNT               = 0x6
	ETHTOOL_A_CHANNELS_TX_COUNT               = 0x7
	ETHTOOL_A_CHANNELS_OTHER_COUNT            = 0x8
	ETHTOOL_A_CHANNELS_COMBINED_COUNT         = 0x9
	ETHTOOL_A_CHANNELS_MAX                    = 0x9
	ETHTOOL_A_COALESCE_UNSPEC                 = 0x0
	ETHTOOL_A_COALESCE_HEADER                 = 0x1
	ETHTOOL_A_COALESCE_RX_USECS               = 0x2
	ETHTOOL_A_COALESCE_RX_MAX_FRAMES          = 0x3
	ETHTOOL_A_COALESCE_RX_USECS_IRQ           = 0x4
	ETHTOOL_A_COALESCE_RX_MAX_FRAMES_IRQ      = 0x5
	ETHTOOL_A_COALESCE_TX_USECS               = 0x6
	ETHTOOL_A_COALESCE_TX_MAX_FRAMES          = 0x7
	ETHTOOL_A_COALESCE_TX_USECS_IRQ           = 0x8
	ETHTOOL_A_COALESCE_TX_MAX_FRAMES_IRQ      = 0x9
	ETHTOOL_A_COALESCE_STATS_BLOCK_USECS      = 0xa
	ETHTOOL_A_COALESCE_USE_ADAPTIVE_RX        = 0xb
	ETHTOOL_A_COALESCE_USE_ADAPTIVE_TX        = 0xc
	ETHTOOL_A_COALESCE_PKT_RATE_LOW           = 0xd
	ETHTOOL_A_COALESCE_RX_USECS_LOW           = 0xe
	ETHTOOL_A_COALESCE_RX_MAX_FRAMES_LOW      = 0xf
	ETHTOOL_A_COALESCE_TX_USECS_LOW           = 0x10
	ETHTOOL_A_COALESCE_TX_MAX_FRAMES_LOW      = 0x11
	ETHTOOL_A_COALESCE_PKT_RATE_HIGH          = 0x12
	ETHTOOL_A_COALESCE_RX_USECS_HIGH          = 0x13
	ETHTOOL_A_COALESCE_RX_MAX_FRAMES_HIGH     = 0x14
	ETHTOOL_A_COALESCE_TX_USECS_HIGH          = 0x15
	ETHTOOL_A_COALESCE_TX_MAX_FRAMES_HIGH     = 0x16
	ETHTOOL_A_COALESCE_RATE_SAMPLE_INTERVAL   = 0x17
	ETHTOOL_A_COALESCE_USE_CQE_MODE_TX        = 0x18
	ETHTOOL_A_COALESCE_USE_CQE_MODE_RX        = 0x19
	ETHTOOL_A_COALESCE_MAX                    = 0x1e
	ETHTOOL_A_PAUSE_UNSPEC                    = 0x0
	ETHTOOL_A_PAUSE_HEADER                    = 0x1
	ETHTOOL_A_PAUSE_AUTONEG                   = 0x2
	ETHTOOL_A_PAUSE_RX                        = 0x3
	ETHTOOL_A_PAUSE_TX                        = 0x4
	ETHTOOL_A_PAUSE_STATS                     = 0x5
	ETHTOOL_A_PAUSE_MAX                       = 0x6
	ETHTOOL_A_PAUSE_STAT_UNSPEC               = 0x0
	ETHTOOL_A_PAUSE_STAT_PAD                  = 0x1
	ETHTOOL_A_PAUSE_STAT_TX_FRAMES            = 0x2
	ETHTOOL_A_PAUSE_STAT_RX_FRAMES            = 0x3
	ETHTOOL_A_PAUSE_STAT_MAX                  = 0x3
	ETHTOOL_A_EEE_UNSPEC                      = 0x0
	ETHTOOL_A_EEE_HEADER                      = 0x1
	ETHTOOL_A_EEE_MODES_OURS                  = 0x2
	ETHTOOL_A_EEE_MODES_PEER                  = 0x3
	ETHTOOL_A_EEE_ACTIVE                      = 0x4
	ETHTOOL_A_EEE_ENABLED                     = 0x5
	ETHTOOL_A_EEE_TX_LPI_ENABLED              = 0x6
	ETHTOOL_A_EEE_TX_LPI_TIMER                = 0x7
	ETHTOOL_A_EEE_MAX                         = 0x7
	ETHTOOL_A_TSINFO_UNSPEC                   = 0x0
	ETHTOOL_A_TSINFO_HEADER                   = 0x1
	ETHTOOL_A_TSINFO_TIMESTAMPING             = 0x2
	ETHTOOL_A_TSINFO_TX_TYPES                 = 0x3
	ETHTOOL_A_TSINFO_RX_FILTERS               = 0x4
	ETHTOOL_A_TSINFO_PHC_INDEX                = 0x5
	ETHTOOL_A_TSINFO_STATS                    = 0x6
	ETHTOOL_A_TSINFO_HWTSTAMP_PROVIDER        = 0x7
	ETHTOOL_A_TSINFO_MAX                      = 0x9
	ETHTOOL_A_CABLE_TEST_UNSPEC               = 0x0
	ETHTOOL_A_CABLE_TEST_HEADER               = 0x1
	ETHTOOL_A_CABLE_TEST_MAX                  = 0x1
	ETHTOOL_A_CABLE_RESULT_CODE_UNSPEC        = 0x0
	ETHTOOL_A_CABLE_RESULT_CODE_OK            = 0x1
	ETHTOOL_A_CABLE_RESULT_CODE_OPEN          = 0x2
	ETHTOOL_A_CABLE_RESULT_CODE_SAME_SHORT    = 0x3
	ETHTOOL_A_CABLE_RESULT_CODE_CROSS_SHORT   = 0x4
	ETHTOOL_A_CABLE_PAIR_A                    = 0x0
	ETHTOOL_A_CABLE_PAIR_B                    = 0x1
	ETHTOOL_A_CABLE_PAIR_C                    = 0x2
	ETHTOOL_A_CABLE_PAIR_D                    = 0x3
	ETHTOOL_A_CABLE_RESULT_UNSPEC             = 0x0
	ETHTOOL_A_CABLE_RESULT_PAIR               = 0x1
	ETHTOOL_A_CABLE_RESULT_CODE               = 0x2
	ETHTOOL_A_CABLE_RESULT_MAX                = 0x3
	ETHTOOL_A_CABLE_FAULT_LENGTH_UNSPEC       = 0x0
	ETHTOOL_A_CABLE_FAULT_LENGTH_PAIR         = 0x1
	ETHTOOL_A_CABLE_FAULT_LENGTH_CM           = 0x2
	ETHTOOL_A_CABLE_FAULT_LENGTH_MAX          = 0x3
	ETHTOOL_A_CABLE_TEST_NTF_STATUS_UNSPEC    = 0x0
	ETHTOOL_A_CABLE_TEST_NTF_STATUS_STARTED   = 0x1
	ETHTOOL_A_CABLE_TEST_NTF_STATUS_COMPLETED = 0x2
	ETHTOOL_A_CABLE_NEST_UNSPEC               = 0x0
	ETHTOOL_A_CABLE_NEST_RESULT               = 0x1
	ETHTOOL_A_CABLE_NEST_FAULT_LENGTH         = 0x2
	ETHTOOL_A_CABLE_NEST_MAX                  = 0x2
	ETHTOOL_A_CABLE_TEST_NTF_UNSPEC           = 0x0
	ETHTOOL_A_CABLE_TEST_NTF_HEADER           = 0x1
	ETHTOOL_A_CABLE_TEST_NTF_STATUS           = 0x2
	ETHTOOL_A_CABLE_TEST_NTF_NEST             = 0x3
	ETHTOOL_A_CABLE_TEST_NTF_MAX              = 0x3
	ETHTOOL_A_CABLE_TEST_TDR_CFG_UNSPEC       = 0x0
	ETHTOOL_A_CABLE_TEST_TDR_CFG_FIRST        = 0x1
	ETHTOOL_A_CABLE_TEST_TDR_CFG_LAST         = 0x2
	ETHTOOL_A_CABLE_TEST_TDR_CFG_STEP         = 0x3
	ETHTOOL_A_CABLE_TEST_TDR_CFG_PAIR         = 0x4
	ETHTOOL_A_CABLE_TEST_TDR_CFG_MAX          = 0x4
	ETHTOOL_A_CABLE_TEST_TDR_UNSPEC           = 0x0
	ETHTOOL_A_CABLE_TEST_TDR_HEADER           = 0x1
	ETHTOOL_A_CABLE_TEST_TDR_CFG              = 0x2
	ETHTOOL_A_CABLE_TEST_TDR_MAX              = 0x2
	ETHTOOL_A_CABLE_AMPLITUDE_UNSPEC          = 0x0
	ETHTOOL_A_CABLE_AMPLITUDE_PAIR            = 0x1
	ETHTOOL_A_CABLE_AMPLITUDE_mV              = 0x2
	ETHTOOL_A_CABLE_AMPLITUDE_MAX             = 0x2
	ETHTOOL_A_CABLE_PULSE_UNSPEC              = 0x0
	ETHTOOL_A_CABLE_PULSE_mV                  = 0x1
	ETHTOOL_A_CABLE_PULSE_MAX                 = 0x1
	ETHTOOL_A_CABLE_STEP_UNSPEC               = 0x0
	ETHTOOL_A_CABLE_STEP_FIRST_DISTANCE       = 0x1
	ETHTOOL_A_CABLE_STEP_LAST_DISTANCE        = 0x2
	ETHTOOL_A_CABLE_STEP_STEP_DISTANCE        = 0x3
	ETHTOOL_A_CABLE_STEP_MAX                  = 0x3
	ETHTOOL_A_CABLE_TDR_NEST_UNSPEC           = 0x0
	ETHTOOL_A_CABLE_TDR_NEST_STEP             = 0x1
	ETHTOOL_A_CABLE_TDR_NEST_AMPLITUDE        = 0x2
	ETHTOOL_A_CABLE_TDR_NEST_PULSE            = 0x3
	ETHTOOL_A_CABLE_TDR_NEST_MAX              = 0x3
	ETHTOOL_A_CABLE_TEST_TDR_NTF_UNSPEC       = 0x0
	ETHTOOL_A_CABLE_TEST_TDR_NTF_HEADER       = 0x1
	ETHTOOL_A_CABLE_TEST_TDR_NTF_STATUS       = 0x2
	ETHTOOL_A_CABLE_TEST_TDR_NTF_NEST         = 0x3
	ETHTOOL_A_CABLE_TEST_TDR_NTF_MAX          = 0x3
	ETHTOOL_UDP_TUNNEL_TYPE_VXLAN             = 0x0
	ETHTOOL_UDP_TUNNEL_TYPE_GENEVE            = 0x1
	ETHTOOL_UDP_TUNNEL_TYPE_VXLAN_GPE         = 0x2
	ETHTOOL_A_TUNNEL_UDP_ENTRY_UNSPEC         = 0x0
	ETHTOOL_A_TUNNEL_UDP_ENTRY_PORT           = 0x1
	ETHTOOL_A_TUNNEL_UDP_ENTRY_TYPE           = 0x2
	ETHTOOL_A_TUNNEL_UDP_ENTRY_MAX            = 0x2
	ETHTOOL_A_TUNNEL_UDP_TABLE_UNSPEC         = 0x0
	ETHTOOL_A_TUNNEL_UDP_TABLE_SIZE           = 0x1
	ETHTOOL_A_TUNNEL_UDP_TABLE_TYPES          = 0x2
	ETHTOOL_A_TUNNEL_UDP_TABLE_ENTRY          = 0x3
	ETHTOOL_A_TUNNEL_UDP_TABLE_MAX            = 0x3
	ETHTOOL_A_TUNNEL_UDP_UNSPEC               = 0x0
	ETHTOOL_A_TUNNEL_UDP_TABLE                = 0x1
	ETHTOOL_A_TUNNEL_UDP_MAX                  = 0x1
	ETHTOOL_A_TUNNEL_INFO_UNSPEC              = 0x0
	ETHTOOL_A_TUNNEL_INFO_HEADER              = 0x1
	ETHTOOL_A_TUNNEL_INFO_UDP_PORTS           = 0x2
	ETHTOOL_A_TUNNEL_INFO_MAX                 = 0x2
)

const (
	TCP_V4_FLOW    = 0x1
	UDP_V4_FLOW    = 0x2
	TCP_V6_FLOW    = 0x5
	UDP_V6_FLOW    = 0x6
	ESP_V4_FLOW    = 0xa
	ESP_V6_FLOW    = 0xc
	IP_USER_FLOW   = 0xd
	IPV6_USER_FLOW = 0xe
	IPV6_FLOW      = 0x11
	ETHER_FLOW     = 0x12
)

const SPEED_UNKNOWN = -0x1

type EthtoolDrvinfo struct {
	Cmd          uint32
	Driver       [32]byte
	Version      [32]byte
	Fw_version   [32]byte
	Bus_info     [32]byte
	Erom_version [32]byte
	Reserved2    [12]byte
	N_priv_flags uint32
	N_stats      uint32
	Testinfo_len uint32
	Eedump_len   uint32
	Regdump_len  uint32
}

type EthtoolTsInfo struct {
	Cmd             uint32
	So_timestamping uint32
	Phc_index       int32
	Tx_types        uint32
	Tx_reserved     [3]uint32
	Rx_filters      uint32
	Rx_reserved     [3]uint32
}

type HwTstampConfig struct {
	Flags     int32
	Tx_type   int32
	Rx_filter int32
}

const (
	HWTSTAMP_FILTER_NONE            = 0x0
	HWTSTAMP_FILTER_ALL             = 0x1
	HWTSTAMP_FILTER_SOME            = 0x2
	HWTSTAMP_FILTER_PTP_V1_L4_EVENT = 0x3
	HWTSTAMP_FILTER_PTP_V2_L4_EVENT = 0x6
	HWTSTAMP_FILTER_PTP_V2_L2_EVENT = 0x9
	HWTSTAMP_FILTER_PTP_V2_EVENT    = 0xc
)

const (
	HWTSTAMP_TX_OFF          = 0x0
	HWTSTAMP_TX_ON           = 0x1
	HWTSTAMP_TX_ONESTEP_SYNC = 0x2
)

type (
	PtpClockCaps struct {
		Max_adj            int32
		N_alarm            int32
		N_ext_ts           int32
		N_per_out          int32
		Pps                int32
		N_pins             int32
		Cross_timestamping int32
		Adjust_phase       int32
		Max_phase_adj      int32
		Rsv                [11]int32
	}
	PtpClockTime struct {
		Sec      int64
		Nsec     uint32
		Reserved uint32
	}
	PtpExttsEvent struct {
		T     PtpClockTime
		Index uint32
		Flags uint32
		Rsv   [2]uint32
	}
	PtpExttsRequest struct {
		Index uint32
		Flags uint32
		Rsv   [2]uint32
	}
	PtpPeroutRequest struct {
		StartOrPhase PtpClockTime
		Period       PtpClockTime
		Index        uint32
		Flags        uint32
		On           PtpClockTime
	}
	PtpPinDesc struct {
		Name  [64]byte
		Index uint32
		Func  uint32
		Chan  uint32
		Rsv   [5]uint32
	}
	PtpSysOffset struct {
		Samples uint32
		Rsv     [3]uint32
		Ts      [51]PtpClockTime
	}
	PtpSysOffsetExtended struct {
		Samples uint32
		Clockid int32
		Rsv     [2]uint32
		Ts      [25][3]PtpClockTime
	}
	PtpSysOffsetPrecise struct {
		Device   PtpClockTime
		Realtime PtpClockTime
		Monoraw  PtpClockTime
		Rsv      [4]uint32
	}
)

const (
	PTP_PF_NONE    = 0x0
	PTP_PF_EXTTS   = 0x1
	PTP_PF_PEROUT  = 0x2
	PTP_PF_PHYSYNC = 0x3
)

type (
	HIDRawReportDescriptor struct {
		Size  uint32
		Value [4096]uint8
	}
	HIDRawDevInfo struct {
		Bustype uint32
		Vendor  int16
		Product int16
	}
)

const (
	CLOSE_RANGE_UNSHARE = 0x2
	CLOSE_RANGE_CLOEXEC = 0x4
)

const (
	NLMSGERR_ATTR_MSG    = 0x1
	NLMSGERR_ATTR_OFFS   = 0x2
	NLMSGERR_ATTR_COOKIE = 0x3
)

type (
	EraseInfo struct {
		Start  uint32
		Length uint32
	}
	EraseInfo64 struct {
		Start  uint64
		Length uint64
	}
	MtdOobBuf struct {
		Start  uint32
		Length uint32
		Ptr    *uint8
	}
	MtdOobBuf64 struct {
		Start  uint64
		Pad    uint32
		Length uint32
		Ptr    uint64
	}
	MtdWriteReq struct {
		Start  uint64
		Len    uint64
		Ooblen uint64
		Data   uint64
		Oob    uint64
		Mode   uint8
		_      [7]uint8
	}
	MtdInfo struct {
		Type      uint8
		Flags     uint32
		Size      uint32
		Erasesize uint32
		Writesize uint32
		Oobsize   uint32
		_         uint64
	}
	RegionInfo struct {
		Offset      uint32
		Erasesize   uint32
		Numblocks   uint32
		Regionindex uint32
	}
	OtpInfo struct {
		Start  uint32
		Length uint32
		Locked uint32
	}
	NandOobinfo struct {
		Useecc   uint32
		Eccbytes uint32
		Oobfree  [8][2]uint32
		Eccpos   [32]uint32
	}
	NandOobfree struct {
		Offset uint32
		Length uint32
	}
	NandEcclayout struct {
		Eccbytes uint32
		Eccpos   [64]uint32
		Oobavail uint32
		Oobfree  [8]NandOobfree
	}
	MtdEccStats struct {
		Corrected uint32
		Failed    uint32
		Badblocks uint32
		Bbtblocks uint32
	}
)

const (
	MTD_OPS_PLACE_OOB = 0x0
	MTD_OPS_AUTO_OOB  = 0x1
	MTD_OPS_RAW       = 0x2
)

const (
	MTD_FILE_MODE_NORMAL      = 0x0
	MTD_FILE_MODE_OTP_FACTORY = 0x1
	MTD_FILE_MODE_OTP_USER    = 0x2
	MTD_FILE_MODE_RAW         = 0x3
)

const (
	NFC_CMD_UNSPEC                    = 0x0
	NFC_CMD_GET_DEVICE                = 0x1
	NFC_CMD_DEV_UP                    = 0x2
	NFC_CMD_DEV_DOWN                  = 0x3
	NFC_CMD_DEP_LINK_UP               = 0x4
	NFC_CMD_DEP_LINK_DOWN             = 0x5
	NFC_CMD_START_POLL                = 0x6
	NFC_CMD_STOP_POLL                 = 0x7
	NFC_CMD_GET_TARGET                = 0x8
	NFC_EVENT_TARGETS_FOUND           = 0x9
	NFC_EVENT_DEVICE_ADDED            = 0xa
	NFC_EVENT_DEVICE_REMOVED          = 0xb
	NFC_EVENT_TARGET_LOST             = 0xc
	NFC_EVENT_TM_ACTIVATED            = 0xd
	NFC_EVENT_TM_DEACTIVATED          = 0xe
	NFC_CMD_LLC_GET_PARAMS            = 0xf
	NFC_CMD_LLC_SET_PARAMS            = 0x10
	NFC_CMD_ENABLE_SE                 = 0x11
	NFC_CMD_DISABLE_SE                = 0x12
	NFC_CMD_LLC_SDREQ                 = 0x13
	NFC_EVENT_LLC_SDRES               = 0x14
	NFC_CMD_FW_DOWNLOAD               = 0x15
	NFC_EVENT_SE_ADDED                = 0x16
	NFC_EVENT_SE_REMOVED              = 0x17
	NFC_EVENT_SE_CONNECTIVITY         = 0x18
	NFC_EVENT_SE_TRANSACTION          = 0x19
	NFC_CMD_GET_SE                    = 0x1a
	NFC_CMD_SE_IO                     = 0x1b
	NFC_CMD_ACTIVATE_TARGET           = 0x1c
	NFC_CMD_VENDOR                    = 0x1d
	NFC_CMD_DEACTIVATE_TARGET         = 0x1e
	NFC_ATTR_UNSPEC                   = 0x0
	NFC_ATTR_DEVICE_INDEX             = 0x1
	NFC_ATTR_DEVICE_NAME              = 0x2
	NFC_ATTR_PROTOCOLS                = 0x3
	NFC_ATTR_TARGET_INDEX             = 0x4
	NFC_ATTR_TARGET_SENS_RES          = 0x5
	NFC_ATTR_TARGET_SEL_RES           = 0x6
	NFC_ATTR_TARGET_NFCID1            = 0x7
	NFC_ATTR_TARGET_SENSB_RES         = 0x8
	NFC_ATTR_TARGET_SENSF_RES         = 0x9
	NFC_ATTR_COMM_MODE                = 0xa
	NFC_ATTR_RF_MODE                  = 0xb
	NFC_ATTR_DEVICE_POWERED           = 0xc
	NFC_ATTR_IM_PROTOCOLS             = 0xd
	NFC_ATTR_TM_PROTOCOLS             = 0xe
	NFC_ATTR_LLC_PARAM_LTO            = 0xf
	NFC_ATTR_LLC_PARAM_RW             = 0x10
	NFC_ATTR_LLC_PARAM_MIUX           = 0x11
	NFC_ATTR_SE                       = 0x12
	NFC_ATTR_LLC_SDP                  = 0x13
	NFC_ATTR_FIRMWARE_NAME            = 0x14
	NFC_ATTR_SE_INDEX                 = 0x15
	NFC_ATTR_SE_TYPE                  = 0x16
	NFC_ATTR_SE_AID                   = 0x17
	NFC_ATTR_FIRMWARE_DOWNLOAD_STATUS = 0x18
	NFC_ATTR_SE_APDU                  = 0x19
	NFC_ATTR_TARGET_ISO15693_DSFID    = 0x1a
	NFC_ATTR_TARGET_ISO15693_UID      = 0x1b
	NFC_ATTR_SE_PARAMS                = 0x1c
	NFC_ATTR_VENDOR_ID                = 0x1d
	NFC_ATTR_VENDOR_SUBCMD            = 0x1e
	NFC_ATTR_VENDOR_DATA              = 0x1f
	NFC_SDP_ATTR_UNSPEC               = 0x0
	NFC_SDP_ATTR_URI                  = 0x1
	NFC_SDP_ATTR_SAP                  = 0x2
)

type LandlockRulesetAttr struct {
	Access_fs  uint64
	Access_net uint64
	Scoped     uint64
}

type LandlockPathBeneathAttr struct {
	Allowed_access uint64
	Parent_fd      int32
}

const (
	LANDLOCK_RULE_PATH_BENEATH = 0x1
)

const (
	IPC_CREAT   = 0x200
	IPC_EXCL    = 0x400
	IPC_NOWAIT  = 0x800
	IPC_PRIVATE = 0x0

	ipc_64 = 0x100
)

const (
	IPC_RMID = 0x0
	IPC_SET  = 0x1
	IPC_STAT = 0x2
)

const (
	SHM_RDONLY = 0x1000
	SHM_RND    = 0x2000
)

type MountAttr struct {
	Attr_set    uint64
	Attr_clr    uint64
	Propagation uint64
	Userns_fd   uint64
}

const (
	WG_CMD_GET_DEVICE                      = 0x0
	WG_CMD_SET_DEVICE                      = 0x1
	WGDEVICE_F_REPLACE_PEERS               = 0x1
	WGDEVICE_A_UNSPEC                      = 0x0
	WGDEVICE_A_IFINDEX                     = 0x1
	WGDEVICE_A_IFNAME                      = 0x2
	WGDEVICE_A_PRIVATE_KEY                 = 0x3
	WGDEVICE_A_PUBLIC_KEY                  = 0x4
	WGDEVICE_A_FLAGS                       = 0x5
	WGDEVICE_A_LISTEN_PORT                 = 0x6
	WGDEVICE_A_FWMARK                      = 0x7
	WGDEVICE_A_PEERS                       = 0x8
	WGPEER_F_REMOVE_ME                     = 0x1
	WGPEER_F_REPLACE_ALLOWEDIPS            = 0x2
	WGPEER_F_UPDATE_ONLY                   = 0x4
	WGPEER_A_UNSPEC                        = 0x0
	WGPEER_A_PUBLIC_KEY                    = 0x1
	WGPEER_A_PRESHARED_KEY                 = 0x2
	WGPEER_A_FLAGS                         = 0x3
	WGPEER_A_ENDPOINT                      = 0x4
	WGPEER_A_PERSISTENT_KEEPALIVE_INTERVAL = 0x5
	WGPEER_A_LAST_HANDSHAKE_TIME           = 0x6
	WGPEER_A_RX_BYTES                      = 0x7
	WGPEER_A_TX_BYTES                      = 0x8
	WGPEER_A_ALLOWEDIPS                    = 0x9
	WGPEER_A_PROTOCOL_VERSION              = 0xa
	WGALLOWEDIP_A_UNSPEC                   = 0x0
	WGALLOWEDIP_A_FAMILY                   = 0x1
	WGALLOWEDIP_A_IPADDR                   = 0x2
	WGALLOWEDIP_A_CIDR_MASK                = 0x3
)

const (
	NL_ATTR_TYPE_INVALID      = 0x0
	NL_ATTR_TYPE_FLAG         = 0x1
	NL_ATTR_TYPE_U8           = 0x2
	NL_ATTR_TYPE_U16          = 0x3
	NL_ATTR_TYPE_U32          = 0x4
	NL_ATTR_TYPE_U64          = 0x5
	NL_ATTR_TYPE_S8           = 0x6
	NL_ATTR_TYPE_S16          = 0x7
	NL_ATTR_TYPE_S32          = 0x8
	NL_ATTR_TYPE_S64          = 0x9
	NL_ATTR_TYPE_BINARY       = 0xa
	NL_ATTR_TYPE_STRING       = 0xb
	NL_ATTR_TYPE_NUL_STRING   = 0xc
	NL_ATTR_TYPE_NESTED       = 0xd
	NL_ATTR_TYPE_NESTED_ARRAY = 0xe
	NL_ATTR_TYPE_BITFIELD32   = 0xf

	NL_POLICY_TYPE_ATTR_UNSPEC          = 0x0
	NL_POLICY_TYPE_ATTR_TYPE            = 0x1
	NL_POLICY_TYPE_ATTR_MIN_VALUE_S     = 0x2
	NL_POLICY_TYPE_ATTR_MAX_VALUE_S     = 0x3
	NL_POLICY_TYPE_ATTR_MIN_VALUE_U     = 0x4
	NL_POLICY_TYPE_ATTR_MAX_VALUE_U     = 0x5
	NL_POLICY_TYPE_ATTR_MIN_LENGTH      = 0x6
	NL_POLICY_TYPE_ATTR_MAX_LENGTH      = 0x7
	NL_POLICY_TYPE_ATTR_POLICY_IDX      = 0x8
	NL_POLICY_TYPE_ATTR_POLICY_MAXTYPE  = 0x9
	NL_POLICY_TYPE_ATTR_BITFIELD32_MASK = 0xa
	NL_POLICY_TYPE_ATTR_PAD             = 0xb
	NL_POLICY_TYPE_ATTR_MASK            = 0xc
	NL_POLICY_TYPE_ATTR_MAX             = 0xc
)

type CANBitTiming struct {
	Bitrate      uint32
	Sample_point uint32
	Tq           uint32
	Prop_seg     uint32
	Phase_seg1   uint32
	Phase_seg2   uint32
	Sjw          uint32
	Brp          uint32
}

type CANBitTimingConst struct {
	Name      [16]uint8
	Tseg1_min uint32
	Tseg1_max uint32
	Tseg2_min uint32
	Tseg2_max uint32
	Sjw_max   uint32
	Brp_min   uint32
	Brp_max   uint32
	Brp_inc   uint32
}

type CANClock struct {
	Freq uint32
}

type CANBusErrorCounters struct {
	Txerr uint16
	Rxerr uint16
}

type CANCtrlMode struct {
	Mask  uint32
	Flags uint32
}

type CANDeviceStats struct {
	Bus_error        uint32
	Error_warning    uint32
	Error_passive    uint32
	Bus_off          uint32
	Arbitration_lost uint32
	Restarts         uint32
}

const (
	CAN_STATE_ERROR_ACTIVE  = 0x0
	CAN_STATE_ERROR_WARNING = 0x1
	CAN_STATE_ERROR_PASSIVE = 0x2
	CAN_STATE_BUS_OFF       = 0x3
	CAN_STATE_STOPPED       = 0x4
	CAN_STATE_SLEEPING      = 0x5
	CAN_STATE_MAX           = 0x6
)

const (
	IFLA_CAN_UNSPEC               = 0x0
	IFLA_CAN_BITTIMING            = 0x1
	IFLA_CAN_BITTIMING_CONST      = 0x2
	IFLA_CAN_CLOCK                = 0x3
	IFLA_CAN_STATE                = 0x4
	IFLA_CAN_CTRLMODE             = 0x5
	IFLA_CAN_RESTART_MS           = 0x6
	IFLA_CAN_RESTART              = 0x7
	IFLA_CAN_BERR_COUNTER         = 0x8
	IFLA_CAN_DATA_BITTIMING       = 0x9
	IFLA_CAN_DATA_BITTIMING_CONST = 0xa
	IFLA_CAN_TERMINATION          = 0xb
	IFLA_CAN_TERMINATION_CONST    = 0xc
	IFLA_CAN_BITRATE_CONST        = 0xd
	IFLA_CAN_DATA_BITRATE_CONST   = 0xe
	IFLA_CAN_BITRATE_MAX          = 0xf
)

type KCMAttach struct {
	Fd     int32
	Bpf_fd int32
}

type KCMUnattach struct {
	Fd int32
}

type KCMClone struct {
	Fd int32
}

const (
	NL80211_AC_BE                                           = 0x2
	NL80211_AC_BK                                           = 0x3
	NL80211_ACL_POLICY_ACCEPT_UNLESS_LISTED                 = 0x0
	NL80211_ACL_POLICY_DENY_UNLESS_LISTED                   = 0x1
	NL80211_AC_VI                                           = 0x1
	NL80211_AC_VO                                           = 0x0
	NL80211_AP_SETTINGS_EXTERNAL_AUTH_SUPPORT               = 0x1
	NL80211_AP_SETTINGS_SA_QUERY_OFFLOAD_SUPPORT            = 0x2
	NL80211_AP_SME_SA_QUERY_OFFLOAD                         = 0x1
	NL80211_ATTR_4ADDR                                      = 0x53
	NL80211_ATTR_ACK                                        = 0x5c
	NL80211_ATTR_ACK_SIGNAL                                 = 0x107
	NL80211_ATTR_ACL_POLICY                                 = 0xa5
	NL80211_ATTR_ADMITTED_TIME                              = 0xd4
	NL80211_ATTR_AIRTIME_WEIGHT                             = 0x112
	NL80211_ATTR_AKM_SUITES                                 = 0x4c
	NL80211_ATTR_AP_ISOLATE                                 = 0x60
	NL80211_ATTR_AP_SETTINGS_FLAGS                          = 0x135
	NL80211_ATTR_ASSOC_SPP_AMSDU                            = 0x14a
	NL80211_ATTR_AUTH_DATA                                  = 0x9c
	NL80211_ATTR_AUTH_TYPE                                  = 0x35
	NL80211_ATTR_BANDS                                      = 0xef
	NL80211_ATTR_BEACON_HEAD                                = 0xe
	NL80211_ATTR_BEACON_INTERVAL                            = 0xc
	NL80211_ATTR_BEACON_TAIL                                = 0xf
	NL80211_ATTR_BG_SCAN_PERIOD                             = 0x98
	NL80211_ATTR_BSS_BASIC_RATES                            = 0x24
	NL80211_ATTR_BSS                                        = 0x2f
	NL80211_ATTR_BSS_CTS_PROT                               = 0x1c
	NL80211_ATTR_BSS_DUMP_INCLUDE_USE_DATA                  = 0x147
	NL80211_ATTR_BSS_HT_OPMODE                              = 0x6d
	NL80211_ATTR_BSSID                                      = 0xf5
	NL80211_ATTR_BSS_SELECT                                 = 0xe3
	NL80211_ATTR_BSS_SHORT_PREAMBLE                         = 0x1d
	NL80211_ATTR_BSS_SHORT_SLOT_TIME                        = 0x1e
	NL80211_ATTR_CENTER_FREQ1                               = 0xa0
	NL80211_ATTR_CENTER_FREQ1_OFFSET                        = 0x123
	NL80211_ATTR_CENTER_FREQ2                               = 0xa1
	NL80211_ATTR_CHANNEL_WIDTH                              = 0x9f
	NL80211_ATTR_CH_SWITCH_BLOCK_TX                         = 0xb8
	NL80211_ATTR_CH_SWITCH_COUNT                            = 0xb7
	NL80211_ATTR_CIPHER_SUITE_GROUP                         = 0x4a
	NL80211_ATTR_CIPHER_SUITES                              = 0x39
	NL80211_ATTR_CIPHER_SUITES_PAIRWISE                     = 0x49
	NL80211_ATTR_CNTDWN_OFFS_BEACON                         = 0xba
	NL80211_ATTR_CNTDWN_OFFS_PRESP                          = 0xbb
	NL80211_ATTR_COALESCE_RULE                              = 0xb6
	NL80211_ATTR_COALESCE_RULE_CONDITION                    = 0x2
	NL80211_ATTR_COALESCE_RULE_DELAY                        = 0x1
	NL80211_ATTR_COALESCE_RULE_MAX                          = 0x3
	NL80211_ATTR_COALESCE_RULE_PKT_PATTERN                  = 0x3
	NL80211_ATTR_COLOR_CHANGE_COLOR                         = 0x130
	NL80211_ATTR_COLOR_CHANGE_COUNT                         = 0x12f
	NL80211_ATTR_COLOR_CHANGE_ELEMS                         = 0x131
	NL80211_ATTR_CONN_FAILED_REASON                         = 0x9b
	NL80211_ATTR_CONTROL_PORT                               = 0x44
	NL80211_ATTR_CONTROL_PORT_ETHERTYPE                     = 0x66
	NL80211_ATTR_CONTROL_PORT_NO_ENCRYPT                    = 0x67
	NL80211_ATTR_CONTROL_PORT_NO_PREAUTH                    = 0x11e
	NL80211_ATTR_CONTROL_PORT_OVER_NL80211                  = 0x108
	NL80211_ATTR_COOKIE                                     = 0x58
	NL80211_ATTR_CQM_BEACON_LOSS_EVENT                      = 0x8
	NL80211_ATTR_CQM                                        = 0x5e
	NL80211_ATTR_CQM_MAX                                    = 0x9
	NL80211_ATTR_CQM_PKT_LOSS_EVENT                         = 0x4
	NL80211_ATTR_CQM_RSSI_HYST                              = 0x2
	NL80211_ATTR_CQM_RSSI_LEVEL                             = 0x9
	NL80211_ATTR_CQM_RSSI_THOLD                             = 0x1
	NL80211_ATTR_CQM_RSSI_THRESHOLD_EVENT                   = 0x3
	NL80211_ATTR_CQM_TXE_INTVL                              = 0x7
	NL80211_ATTR_CQM_TXE_PKTS                               = 0x6
	NL80211_ATTR_CQM_TXE_RATE                               = 0x5
	NL80211_ATTR_CRIT_PROT_ID                               = 0xb3
	NL80211_ATTR_CSA_C_OFF_BEACON                           = 0xba
	NL80211_ATTR_CSA_C_OFF_PRESP                            = 0xbb
	NL80211_ATTR_CSA_C_OFFSETS_TX                           = 0xcd
	NL80211_ATTR_CSA_IES                                    = 0xb9
	NL80211_ATTR_DEVICE_AP_SME                              = 0x8d
	NL80211_ATTR_DFS_CAC_TIME                               = 0x7
	NL80211_ATTR_DFS_REGION                                 = 0x92
	NL80211_ATTR_DISABLE_EHT                                = 0x137
	NL80211_ATTR_DISABLE_HE                                 = 0x12d
	NL80211_ATTR_DISABLE_HT                                 = 0x93
	NL80211_ATTR_DISABLE_VHT                                = 0xaf
	NL80211_ATTR_DISCONNECTED_BY_AP                         = 0x47
	NL80211_ATTR_DONT_WAIT_FOR_ACK                          = 0x8e
	NL80211_ATTR_DTIM_PERIOD                                = 0xd
	NL80211_ATTR_DURATION                                   = 0x57
	NL80211_ATTR_EHT_CAPABILITY                             = 0x136
	NL80211_ATTR_EMA_RNR_ELEMS                              = 0x145
	NL80211_ATTR_EML_CAPABILITY                             = 0x13d
	NL80211_ATTR_EXT_CAPA                                   = 0xa9
	NL80211_ATTR_EXT_CAPA_MASK                              = 0xaa
	NL80211_ATTR_EXTERNAL_AUTH_ACTION                       = 0x104
	NL80211_ATTR_EXTERNAL_AUTH_SUPPORT                      = 0x105
	NL80211_ATTR_EXT_FEATURES                               = 0xd9
	NL80211_ATTR_FEATURE_FLAGS                              = 0x8f
	NL80211_ATTR_FILS_CACHE_ID                              = 0xfd
	NL80211_ATTR_FILS_DISCOVERY                             = 0x126
	NL80211_ATTR_FILS_ERP_NEXT_SEQ_NUM                      = 0xfb
	NL80211_ATTR_FILS_ERP_REALM                             = 0xfa
	NL80211_ATTR_FILS_ERP_RRK                               = 0xfc
	NL80211_ATTR_FILS_ERP_USERNAME                          = 0xf9
	NL80211_ATTR_FILS_KEK                                   = 0xf2
	NL80211_ATTR_FILS_NONCES                                = 0xf3
	NL80211_ATTR_FRAME                                      = 0x33
	NL80211_ATTR_FRAME_MATCH                                = 0x5b
	NL80211_ATTR_FRAME_TYPE                                 = 0x65
	NL80211_ATTR_FREQ_AFTER                                 = 0x3b
	NL80211_ATTR_FREQ_BEFORE                                = 0x3a
	NL80211_ATTR_FREQ_FIXED                                 = 0x3c
	NL80211_ATTR_FREQ_RANGE_END                             = 0x3
	NL80211_ATTR_FREQ_RANGE_MAX_BW                          = 0x4
	NL80211_ATTR_FREQ_RANGE_START                           = 0x2
	NL80211_ATTR_FTM_RESPONDER                              = 0x10e
	NL80211_ATTR_FTM_RESPONDER_STATS                        = 0x10f
	NL80211_ATTR_GENERATION                                 = 0x2e
	NL80211_ATTR_HANDLE_DFS                                 = 0xbf
	NL80211_ATTR_HE_6GHZ_CAPABILITY                         = 0x125
	NL80211_ATTR_HE_BSS_COLOR                               = 0x11b
	NL80211_ATTR_HE_CAPABILITY                              = 0x10d
	NL80211_ATTR_HE_OBSS_PD                                 = 0x117
	NL80211_ATTR_HIDDEN_SSID                                = 0x7e
	NL80211_ATTR_HT_CAPABILITY                              = 0x1f
	NL80211_ATTR_HT_CAPABILITY_MASK                         = 0x94
	NL80211_ATTR_HW_TIMESTAMP_ENABLED                       = 0x144
	NL80211_ATTR_IE_ASSOC_RESP                              = 0x80
	NL80211_ATTR_IE                                         = 0x2a
	NL80211_ATTR_IE_PROBE_RESP                              = 0x7f
	NL80211_ATTR_IE_RIC                                     = 0xb2
	NL80211_ATTR_IFACE_SOCKET_OWNER                         = 0xcc
	NL80211_ATTR_IFINDEX                                    = 0x3
	NL80211_ATTR_IFNAME                                     = 0x4
	NL80211_ATTR_IFTYPE_AKM_SUITES                          = 0x11c
	NL80211_ATTR_IFTYPE                                     = 0x5
	NL80211_ATTR_IFTYPE_EXT_CAPA                            = 0xe6
	NL80211_ATTR_INACTIVITY_TIMEOUT                         = 0x96
	NL80211_ATTR_INTERFACE_COMBINATIONS                     = 0x78
	NL80211_ATTR_KEY_CIPHER                                 = 0x9
	NL80211_ATTR_KEY                                        = 0x50
	NL80211_ATTR_KEY_DATA                                   = 0x7
	NL80211_ATTR_KEY_DEFAULT                                = 0xb
	NL80211_ATTR_KEY_DEFAULT_MGMT                           = 0x28
	NL80211_ATTR_KEY_DEFAULT_TYPES                          = 0x6e
	NL80211_ATTR_KEY_IDX                                    = 0x8
	NL80211_ATTR_KEYS                                       = 0x51
	NL80211_ATTR_KEY_SEQ                                    = 0xa
	NL80211_ATTR_KEY_TYPE                                   = 0x37
	NL80211_ATTR_LOCAL_MESH_POWER_MODE                      = 0xa4
	NL80211_ATTR_LOCAL_STATE_CHANGE                         = 0x5f
	NL80211_ATTR_MAC_ACL_MAX                                = 0xa7
	NL80211_ATTR_MAC_ADDRS                                  = 0xa6
	NL80211_ATTR_MAC                                        = 0x6
	NL80211_ATTR_MAC_HINT                                   = 0xc8
	NL80211_ATTR_MAC_MASK                                   = 0xd7
	NL80211_ATTR_MAX_AP_ASSOC_STA                           = 0xca
	NL80211_ATTR_MAX                                        = 0x151
	NL80211_ATTR_MAX_CRIT_PROT_DURATION                     = 0xb4
	NL80211_ATTR_MAX_CSA_COUNTERS                           = 0xce
	NL80211_ATTR_MAX_HW_TIMESTAMP_PEERS                     = 0x143
	NL80211_ATTR_MAX_MATCH_SETS                             = 0x85
	NL80211_ATTR_MAX_NUM_AKM_SUITES                         = 0x13c
	NL80211_ATTR_MAX_NUM_PMKIDS                             = 0x56
	NL80211_ATTR_MAX_NUM_SCAN_SSIDS                         = 0x2b
	NL80211_ATTR_MAX_NUM_SCHED_SCAN_PLANS                   = 0xde
	NL80211_ATTR_MAX_NUM_SCHED_SCAN_SSIDS                   = 0x7b
	NL80211_ATTR_MAX_REMAIN_ON_CHANNEL_DURATION             = 0x6f
	NL80211_ATTR_MAX_SCAN_IE_LEN                            = 0x38
	NL80211_ATTR_MAX_SCAN_PLAN_INTERVAL                     = 0xdf
	NL80211_ATTR_MAX_SCAN_PLAN_ITERATIONS                   = 0xe0
	NL80211_ATTR_MAX_SCHED_SCAN_IE_LEN                      = 0x7c
	NL80211_ATTR_MBSSID_CONFIG                              = 0x132
	NL80211_ATTR_MBSSID_ELEMS                               = 0x133
	NL80211_ATTR_MCAST_RATE                                 = 0x6b
	NL80211_ATTR_MDID                                       = 0xb1
	NL80211_ATTR_MEASUREMENT_DURATION                       = 0xeb
	NL80211_ATTR_MEASUREMENT_DURATION_MANDATORY             = 0xec
	NL80211_ATTR_MESH_CONFIG                                = 0x23
	NL80211_ATTR_MESH_ID                                    = 0x18
	NL80211_ATTR_MESH_PEER_AID                              = 0xed
	NL80211_ATTR_MESH_SETUP                                 = 0x70
	NL80211_ATTR_MGMT_SUBTYPE                               = 0x29
	NL80211_ATTR_MLD_ADDR                                   = 0x13a
	NL80211_ATTR_MLD_CAPA_AND_OPS                           = 0x13e
	NL80211_ATTR_MLO_LINK_DISABLED                          = 0x146
	NL80211_ATTR_MLO_LINK_ID                                = 0x139
	NL80211_ATTR_MLO_LINKS                                  = 0x138
	NL80211_ATTR_MLO_SUPPORT                                = 0x13b
	NL80211_ATTR_MLO_TTLM_DLINK                             = 0x148
	NL80211_ATTR_MLO_TTLM_ULINK                             = 0x149
	NL80211_ATTR_MNTR_FLAGS                                 = 0x17
	NL80211_ATTR_MPATH_INFO                                 = 0x1b
	NL80211_ATTR_MPATH_NEXT_HOP                             = 0x1a
	NL80211_ATTR_MULTICAST_TO_UNICAST_ENABLED               = 0xf4
	NL80211_ATTR_MU_MIMO_FOLLOW_MAC_ADDR                    = 0xe8
	NL80211_ATTR_MU_MIMO_GROUP_DATA                         = 0xe7
	NL80211_ATTR_NAN_FUNC                                   = 0xf0
	NL80211_ATTR_NAN_MASTER_PREF                            = 0xee
	NL80211_ATTR_NAN_MATCH                                  = 0xf1
	NL80211_ATTR_NETNS_FD                                   = 0xdb
	NL80211_ATTR_NOACK_MAP                                  = 0x95
	NL80211_ATTR_NSS                                        = 0x106
	NL80211_ATTR_OBSS_COLOR_BITMAP                          = 0x12e
	NL80211_ATTR_OFFCHANNEL_TX_OK                           = 0x6c
	NL80211_ATTR_OPER_CLASS                                 = 0xd6
	NL80211_ATTR_OPMODE_NOTIF                               = 0xc2
	NL80211_ATTR_P2P_CTWINDOW                               = 0xa2
	NL80211_ATTR_P2P_OPPPS                                  = 0xa3
	NL80211_ATTR_PAD                                        = 0xe5
	NL80211_ATTR_PBSS                                       = 0xe2
	NL80211_ATTR_PEER_AID                                   = 0xb5
	NL80211_ATTR_PEER_MEASUREMENTS                          = 0x111
	NL80211_ATTR_PID                                        = 0x52
	NL80211_ATTR_PMK                                        = 0xfe
	NL80211_ATTR_PMKID                                      = 0x55
	NL80211_ATTR_PMK_LIFETIME                               = 0x11f
	NL80211_ATTR_PMKR0_NAME                                 = 0x102
	NL80211_ATTR_PMK_REAUTH_THRESHOLD                       = 0x120
	NL80211_ATTR_PMKSA_CANDIDATE                            = 0x86
	NL80211_ATTR_PORT_AUTHORIZED                            = 0x103
	NL80211_ATTR_POWER_RULE_MAX_ANT_GAIN                    = 0x5
	NL80211_ATTR_POWER_RULE_MAX_EIRP                        = 0x6
	NL80211_ATTR_POWER_RULE_PSD                             = 0x8
	NL80211_ATTR_PREV_BSSID                                 = 0x4f
	NL80211_ATTR_PRIVACY                                    = 0x46
	NL80211_ATTR_PROBE_RESP                                 = 0x91
	NL80211_ATTR_PROBE_RESP_OFFLOAD                         = 0x90
	NL80211_ATTR_PROTOCOL_FEATURES                          = 0xad
	NL80211_ATTR_PS_STATE                                   = 0x5d
	NL80211_ATTR_PUNCT_BITMAP                               = 0x142
	NL80211_ATTR_QOS_MAP                                    = 0xc7
	NL80211_ATTR_RADAR_BACKGROUND                           = 0x134
	NL80211_ATTR_RADAR_EVENT                                = 0xa8
	NL80211_ATTR_REASON_CODE                                = 0x36
	NL80211_ATTR_RECEIVE_MULTICAST                          = 0x121
	NL80211_ATTR_RECONNECT_REQUESTED                        = 0x12b
	NL80211_ATTR_REG_ALPHA2                                 = 0x21
	NL80211_ATTR_REG_INDOOR                                 = 0xdd
	NL80211_ATTR_REG_INITIATOR                              = 0x30
	NL80211_ATTR_REG_RULE_FLAGS                             = 0x1
	NL80211_ATTR_REG_RULES                                  = 0x22
	NL80211_ATTR_REG_TYPE                                   = 0x31
	NL80211_ATTR_REKEY_DATA                                 = 0x7a
	NL80211_ATTR_REQ_IE                                     = 0x4d
	NL80211_ATTR_RESP_IE                                    = 0x4e
	NL80211_ATTR_ROAM_SUPPORT                               = 0x83
	NL80211_ATTR_RX_FRAME_TYPES                             = 0x64
	NL80211_ATTR_RX_HW_TIMESTAMP                            = 0x140
	NL80211_ATTR_RXMGMT_FLAGS                               = 0xbc
	NL80211_ATTR_RX_SIGNAL_DBM                              = 0x97
	NL80211_ATTR_S1G_CAPABILITY                             = 0x128
	NL80211_ATTR_S1G_CAPABILITY_MASK                        = 0x129
	NL80211_ATTR_SAE_DATA                                   = 0x9c
	NL80211_ATTR_SAE_PASSWORD                               = 0x115
	NL80211_ATTR_SAE_PWE                                    = 0x12a
	NL80211_ATTR_SAR_SPEC                                   = 0x12c
	NL80211_ATTR_SCAN_FLAGS                                 = 0x9e
	NL80211_ATTR_SCAN_FREQ_KHZ                              = 0x124
	NL80211_ATTR_SCAN_FREQUENCIES                           = 0x2c
	NL80211_ATTR_SCAN_GENERATION                            = 0x2e
	NL80211_ATTR_SCAN_SSIDS                                 = 0x2d
	NL80211_ATTR_SCAN_START_TIME_TSF_BSSID                  = 0xea
	NL80211_ATTR_SCAN_START_TIME_TSF                        = 0xe9
	NL80211_ATTR_SCAN_SUPP_RATES                            = 0x7d
	NL80211_ATTR_SCHED_SCAN_DELAY                           = 0xdc
	NL80211_ATTR_SCHED_SCAN_INTERVAL                        = 0x77
	NL80211_ATTR_SCHED_SCAN_MATCH                           = 0x84
	NL80211_ATTR_SCHED_SCAN_MATCH_SSID                      = 0x1
	NL80211_ATTR_SCHED_SCAN_MAX_REQS                        = 0x100
	NL80211_ATTR_SCHED_SCAN_MULTI                           = 0xff
	NL80211_ATTR_SCHED_SCAN_PLANS                           = 0xe1
	NL80211_ATTR_SCHED_SCAN_RELATIVE_RSSI                   = 0xf6
	NL80211_ATTR_SCHED_SCAN_RSSI_ADJUST                     = 0xf7
	NL80211_ATTR_SMPS_MODE                                  = 0xd5
	NL80211_ATTR_SOCKET_OWNER                               = 0xcc
	NL80211_ATTR_SOFTWARE_IFTYPES                           = 0x79
	NL80211_ATTR_SPLIT_WIPHY_DUMP                           = 0xae
	NL80211_ATTR_SSID                                       = 0x34
	NL80211_ATTR_STA_AID                                    = 0x10
	NL80211_ATTR_STA_CAPABILITY                             = 0xab
	NL80211_ATTR_STA_EXT_CAPABILITY                         = 0xac
	NL80211_ATTR_STA_FLAGS2                                 = 0x43
	NL80211_ATTR_STA_FLAGS                                  = 0x11
	NL80211_ATTR_STA_INFO                                   = 0x15
	NL80211_ATTR_STA_LISTEN_INTERVAL                        = 0x12
	NL80211_ATTR_STA_PLINK_ACTION                           = 0x19
	NL80211_ATTR_STA_PLINK_STATE                            = 0x74
	NL80211_ATTR_STA_SUPPORTED_CHANNELS                     = 0xbd
	NL80211_ATTR_STA_SUPPORTED_OPER_CLASSES                 = 0xbe
	NL80211_ATTR_STA_SUPPORTED_RATES                        = 0x13
	NL80211_ATTR_STA_SUPPORT_P2P_PS                         = 0xe4
	NL80211_ATTR_STATUS_CODE                                = 0x48
	NL80211_ATTR_STA_TX_POWER                               = 0x114
	NL80211_ATTR_STA_TX_POWER_SETTING                       = 0x113
	NL80211_ATTR_STA_VLAN                                   = 0x14
	NL80211_ATTR_STA_WME                                    = 0x81
	NL80211_ATTR_SUPPORT_10_MHZ                             = 0xc1
	NL80211_ATTR_SUPPORT_5_MHZ                              = 0xc0
	NL80211_ATTR_SUPPORT_AP_UAPSD                           = 0x82
	NL80211_ATTR_SUPPORTED_COMMANDS                         = 0x32
	NL80211_ATTR_SUPPORTED_IFTYPES                          = 0x20
	NL80211_ATTR_SUPPORT_IBSS_RSN                           = 0x68
	NL80211_ATTR_SUPPORT_MESH_AUTH                          = 0x73
	NL80211_ATTR_SURVEY_INFO                                = 0x54
	NL80211_ATTR_SURVEY_RADIO_STATS                         = 0xda
	NL80211_ATTR_TD_BITMAP                                  = 0x141
	NL80211_ATTR_TDLS_ACTION                                = 0x88
	NL80211_ATTR_TDLS_DIALOG_TOKEN                          = 0x89
	NL80211_ATTR_TDLS_EXTERNAL_SETUP                        = 0x8c
	NL80211_ATTR_TDLS_INITIATOR                             = 0xcf
	NL80211_ATTR_TDLS_OPERATION                             = 0x8a
	NL80211_ATTR_TDLS_PEER_CAPABILITY                       = 0xcb
	NL80211_ATTR_TDLS_SUPPORT                               = 0x8b
	NL80211_ATTR_TESTDATA                                   = 0x45
	NL80211_ATTR_TID_CONFIG                                 = 0x11d
	NL80211_ATTR_TIMED_OUT                                  = 0x41
	NL80211_ATTR_TIMEOUT                                    = 0x110
	NL80211_ATTR_TIMEOUT_REASON                             = 0xf8
	NL80211_ATTR_TSID                                       = 0xd2
	NL80211_ATTR_TWT_RESPONDER                              = 0x116
	NL80211_ATTR_TX_FRAME_TYPES                             = 0x63
	NL80211_ATTR_TX_HW_TIMESTAMP                            = 0x13f
	NL80211_ATTR_TX_NO_CCK_RATE                             = 0x87
	NL80211_ATTR_TXQ_LIMIT                                  = 0x10a
	NL80211_ATTR_TXQ_MEMORY_LIMIT                           = 0x10b
	NL80211_ATTR_TXQ_QUANTUM                                = 0x10c
	NL80211_ATTR_TXQ_STATS                                  = 0x109
	NL80211_ATTR_TX_RATES                                   = 0x5a
	NL80211_ATTR_UNSOL_BCAST_PROBE_RESP                     = 0x127
	NL80211_ATTR_UNSPEC                                     = 0x0
	NL80211_ATTR_USE_MFP                                    = 0x42
	NL80211_ATTR_USER_PRIO                                  = 0xd3
	NL80211_ATTR_USER_REG_HINT_TYPE                         = 0x9a
	NL80211_ATTR_USE_RRM                                    = 0xd0
	NL80211_ATTR_VENDOR_DATA                                = 0xc5
	NL80211_ATTR_VENDOR_EVENTS                              = 0xc6
	NL80211_ATTR_VENDOR_ID                                  = 0xc3
	NL80211_ATTR_VENDOR_SUBCMD                              = 0xc4
	NL80211_ATTR_VHT_CAPABILITY                             = 0x9d
	NL80211_ATTR_VHT_CAPABILITY_MASK                        = 0xb0
	NL80211_ATTR_VLAN_ID                                    = 0x11a
	NL80211_ATTR_WANT_1X_4WAY_HS                            = 0x101
	NL80211_ATTR_WDEV                                       = 0x99
	NL80211_ATTR_WIPHY_ANTENNA_AVAIL_RX                     = 0x72
	NL80211_ATTR_WIPHY_ANTENNA_AVAIL_TX                     = 0x71
	NL80211_ATTR_WIPHY_ANTENNA_RX                           = 0x6a
	NL80211_ATTR_WIPHY_ANTENNA_TX                           = 0x69
	NL80211_ATTR_WIPHY_BANDS                                = 0x16
	NL80211_ATTR_WIPHY_CHANNEL_TYPE                         = 0x27
	NL80211_ATTR_WIPHY                                      = 0x1
	NL80211_ATTR_WIPHY_COVERAGE_CLASS                       = 0x59
	NL80211_ATTR_WIPHY_DYN_ACK                              = 0xd1
	NL80211_ATTR_WIPHY_EDMG_BW_CONFIG                       = 0x119
	NL80211_ATTR_WIPHY_EDMG_CHANNELS                        = 0x118
	NL80211_ATTR_WIPHY_FRAG_THRESHOLD                       = 0x3f
	NL80211_ATTR_WIPHY_FREQ                                 = 0x26
	NL80211_ATTR_WIPHY_FREQ_HINT                            = 0xc9
	NL80211_ATTR_WIPHY_FREQ_OFFSET                          = 0x122
	NL80211_ATTR_WIPHY_INTERFACE_COMBINATIONS               = 0x14c
	NL80211_ATTR_WIPHY_NAME                                 = 0x2
	NL80211_ATTR_WIPHY_RADIOS                               = 0x14b
	NL80211_ATTR_WIPHY_RETRY_LONG                           = 0x3e
	NL80211_ATTR_WIPHY_RETRY_SHORT                          = 0x3d
	NL80211_ATTR_WIPHY_RTS_THRESHOLD                        = 0x40
	NL80211_ATTR_WIPHY_SELF_MANAGED_REG                     = 0xd8
	NL80211_ATTR_WIPHY_TX_POWER_LEVEL                       = 0x62
	NL80211_ATTR_WIPHY_TX_POWER_SETTING                     = 0x61
	NL80211_ATTR_WIPHY_TXQ_PARAMS                           = 0x25
	NL80211_ATTR_WOWLAN_TRIGGERS                            = 0x75
	NL80211_ATTR_WOWLAN_TRIGGERS_SUPPORTED                  = 0x76
	NL80211_ATTR_WPA_VERSIONS                               = 0x4b
	NL80211_AUTHTYPE_AUTOMATIC                              = 0x8
	NL80211_AUTHTYPE_FILS_PK                                = 0x7
	NL80211_AUTHTYPE_FILS_SK                                = 0x5
	NL80211_AUTHTYPE_FILS_SK_PFS                            = 0x6
	NL80211_AUTHTYPE_FT                                     = 0x2
	NL80211_AUTHTYPE_MAX                                    = 0x7
	NL80211_AUTHTYPE_NETWORK_EAP                            = 0x3
	NL80211_AUTHTYPE_OPEN_SYSTEM                            = 0x0
	NL80211_AUTHTYPE_SAE                                    = 0x4
	NL80211_AUTHTYPE_SHARED_KEY                             = 0x1
	NL80211_BAND_2GHZ                                       = 0x0
	NL80211_BAND_5GHZ                                       = 0x1
	NL80211_BAND_60GHZ                                      = 0x2
	NL80211_BAND_6GHZ                                       = 0x3
	NL80211_BAND_ATTR_EDMG_BW_CONFIG                        = 0xb
	NL80211_BAND_ATTR_EDMG_CHANNELS                         = 0xa
	NL80211_BAND_ATTR_FREQS                                 = 0x1
	NL80211_BAND_ATTR_HT_AMPDU_DENSITY                      = 0x6
	NL80211_BAND_ATTR_HT_AMPDU_FACTOR                       = 0x5
	NL80211_BAND_ATTR_HT_CAPA                               = 0x4
	NL80211_BAND_ATTR_HT_MCS_SET                            = 0x3
	NL80211_BAND_ATTR_IFTYPE_DATA                           = 0x9
	NL80211_BAND_ATTR_MAX                                   = 0xd
	NL80211_BAND_ATTR_RATES                                 = 0x2
	NL80211_BAND_ATTR_S1G_CAPA                              = 0xd
	NL80211_BAND_ATTR_S1G_MCS_NSS_SET                       = 0xc
	NL80211_BAND_ATTR_VHT_CAPA                              = 0x8
	NL80211_BAND_ATTR_VHT_MCS_SET                           = 0x7
	NL80211_BAND_IFTYPE_ATTR_EHT_CAP_MAC                    = 0x8
	NL80211_BAND_IFTYPE_ATTR_EHT_CAP_MCS_SET                = 0xa
	NL80211_BAND_IFTYPE_ATTR_EHT_CAP_PHY                    = 0x9
	NL80211_BAND_IFTYPE_ATTR_EHT_CAP_PPE                    = 0xb
	NL80211_BAND_IFTYPE_ATTR_HE_6GHZ_CAPA                   = 0x6
	NL80211_BAND_IFTYPE_ATTR_HE_CAP_MAC                     = 0x2
	NL80211_BAND_IFTYPE_ATTR_HE_CAP_MCS_SET                 = 0x4
	NL80211_BAND_IFTYPE_ATTR_HE_CAP_PHY                     = 0x3
	NL80211_BAND_IFTYPE_ATTR_HE_CAP_PPE                     = 0x5
	NL80211_BAND_IFTYPE_ATTR_IFTYPES                        = 0x1
	NL80211_BAND_IFTYPE_ATTR_MAX                            = 0xb
	NL80211_BAND_IFTYPE_ATTR_VENDOR_ELEMS                   = 0x7
	NL80211_BAND_LC                                         = 0x5
	NL80211_BAND_S1GHZ                                      = 0x4
	NL80211_BITRATE_ATTR_2GHZ_SHORTPREAMBLE                 = 0x2
	NL80211_BITRATE_ATTR_MAX                                = 0x2
	NL80211_BITRATE_ATTR_RATE                               = 0x1
	NL80211_BSS_BEACON_IES                                  = 0xb
	NL80211_BSS_BEACON_INTERVAL                             = 0x4
	NL80211_BSS_BEACON_TSF                                  = 0xd
	NL80211_BSS_BSSID                                       = 0x1
	NL80211_BSS_CANNOT_USE_6GHZ_PWR_MISMATCH                = 0x2
	NL80211_BSS_CANNOT_USE_NSTR_NONPRIMARY                  = 0x1
	NL80211_BSS_CANNOT_USE_REASONS                          = 0x18
	NL80211_BSS_CANNOT_USE_UHB_PWR_MISMATCH                 = 0x2
	NL80211_BSS_CAPABILITY                                  = 0x5
	NL80211_BSS_CHAIN_SIGNAL                                = 0x13
	NL80211_BSS_CHAN_WIDTH_10                               = 0x1
	NL80211_BSS_CHAN_WIDTH_1                                = 0x3
	NL80211_BSS_CHAN_WIDTH_20                               = 0x0
	NL80211_BSS_CHAN_WIDTH_2                                = 0x4
	NL80211_BSS_CHAN_WIDTH_5                                = 0x2
	NL80211_BSS_CHAN_WIDTH                                  = 0xc
	NL80211_BSS_FREQUENCY                                   = 0x2
	NL80211_BSS_FREQUENCY_OFFSET                            = 0x14
	NL80211_BSS_INFORMATION_ELEMENTS                        = 0x6
	NL80211_BSS_LAST_SEEN_BOOTTIME                          = 0xf
	NL80211_BSS_MAX                                         = 0x18
	NL80211_BSS_MLD_ADDR                                    = 0x16
	NL80211_BSS_MLO_LINK_ID                                 = 0x15
	NL80211_BSS_PAD                                         = 0x10
	NL80211_BSS_PARENT_BSSID                                = 0x12
	NL80211_BSS_PARENT_TSF                                  = 0x11
	NL80211_BSS_PRESP_DATA                                  = 0xe
	NL80211_BSS_SEEN_MS_AGO                                 = 0xa
	NL80211_BSS_SELECT_ATTR_BAND_PREF                       = 0x2
	NL80211_BSS_SELECT_ATTR_MAX                             = 0x3
	NL80211_BSS_SELECT_ATTR_RSSI_ADJUST                     = 0x3
	NL80211_BSS_SELECT_ATTR_RSSI                            = 0x1
	NL80211_BSS_SIGNAL_MBM                                  = 0x7
	NL80211_BSS_SIGNAL_UNSPEC                               = 0x8
	NL80211_BSS_STATUS_ASSOCIATED                           = 0x1
	NL80211_BSS_STATUS_AUTHENTICATED                        = 0x0
	NL80211_BSS_STATUS                                      = 0x9
	NL80211_BSS_STATUS_IBSS_JOINED                          = 0x2
	NL80211_BSS_TSF                                         = 0x3
	NL80211_BSS_USE_FOR                                     = 0x17
	NL80211_BSS_USE_FOR_MLD_LINK                            = 0x2
	NL80211_BSS_USE_FOR_NORMAL                              = 0x1
	NL80211_CHAN_HT20                                       = 0x1
	NL80211_CHAN_HT40MINUS                                  = 0x2
	NL80211_CHAN_HT40PLUS                                   = 0x3
	NL80211_CHAN_NO_HT                                      = 0x0
	NL80211_CHAN_WIDTH_10                                   = 0x7
	NL80211_CHAN_WIDTH_160                                  = 0x5
	NL80211_CHAN_WIDTH_16                                   = 0xc
	NL80211_CHAN_WIDTH_1                                    = 0x8
	NL80211_CHAN_WIDTH_20                                   = 0x1
	NL80211_CHAN_WIDTH_20_NOHT                              = 0x0
	NL80211_CHAN_WIDTH_2                                    = 0x9
	NL80211_CHAN_WIDTH_320                                  = 0xd
	NL80211_CHAN_WIDTH_40                                   = 0x2
	NL80211_CHAN_WIDTH_4                                    = 0xa
	NL80211_CHAN_WIDTH_5                                    = 0x6
	NL80211_CHAN_WIDTH_80                                   = 0x3
	NL80211_CHAN_WIDTH_80P80                                = 0x4
	NL80211_CHAN_WIDTH_8                                    = 0xb
	NL80211_CMD_ABORT_SCAN                                  = 0x72
	NL80211_CMD_ACTION                                      = 0x3b
	NL80211_CMD_ACTION_TX_STATUS                            = 0x3c
	NL80211_CMD_ADD_LINK                                    = 0x94
	NL80211_CMD_ADD_LINK_STA                                = 0x96
	NL80211_CMD_ADD_NAN_FUNCTION                            = 0x75
	NL80211_CMD_ADD_TX_TS                                   = 0x69
	NL80211_CMD_ASSOC_COMEBACK                              = 0x93
	NL80211_CMD_ASSOCIATE                                   = 0x26
	NL80211_CMD_AUTHENTICATE                                = 0x25
	NL80211_CMD_CANCEL_REMAIN_ON_CHANNEL                    = 0x38
	NL80211_CMD_CHANGE_NAN_CONFIG                           = 0x77
	NL80211_CMD_CHANNEL_SWITCH                              = 0x66
	NL80211_CMD_CH_SWITCH_NOTIFY                            = 0x58
	NL80211_CMD_CH_SWITCH_STARTED_NOTIFY                    = 0x6e
	NL80211_CMD_COLOR_CHANGE_ABORTED                        = 0x90
	NL80211_CMD_COLOR_CHANGE_COMPLETED                      = 0x91
	NL80211_CMD_COLOR_CHANGE_REQUEST                        = 0x8e
	NL80211_CMD_COLOR_CHANGE_STARTED                        = 0x8f
	NL80211_CMD_CONNECT                                     = 0x2e
	NL80211_CMD_CONN_FAILED                                 = 0x5b
	NL80211_CMD_CONTROL_PORT_FRAME                          = 0x81
	NL80211_CMD_CONTROL_PORT_FRAME_TX_STATUS                = 0x8b
	NL80211_CMD_CRIT_PROTOCOL_START                         = 0x62
	NL80211_CMD_CRIT_PROTOCOL_STOP                          = 0x63
	NL80211_CMD_DEAUTHENTICATE                              = 0x27
	NL80211_CMD_DEL_BEACON                                  = 0x10
	NL80211_CMD_DEL_INTERFACE                               = 0x8
	NL80211_CMD_DEL_KEY                                     = 0xc
	NL80211_CMD_DEL_MPATH                                   = 0x18
	NL80211_CMD_DEL_NAN_FUNCTION                            = 0x76
	NL80211_CMD_DEL_PMK                                     = 0x7c
	NL80211_CMD_DEL_PMKSA                                   = 0x35
	NL80211_CMD_DEL_STATION                                 = 0x14
	NL80211_CMD_DEL_TX_TS                                   = 0x6a
	NL80211_CMD_DEL_WIPHY                                   = 0x4
	NL80211_CMD_DISASSOCIATE                                = 0x28
	NL80211_CMD_DISCONNECT                                  = 0x30
	NL80211_CMD_EXTERNAL_AUTH                               = 0x7f
	NL80211_CMD_FLUSH_PMKSA                                 = 0x36
	NL80211_CMD_FRAME                                       = 0x3b
	NL80211_CMD_FRAME_TX_STATUS                             = 0x3c
	NL80211_CMD_FRAME_WAIT_CANCEL                           = 0x43
	NL80211_CMD_FT_EVENT                                    = 0x61
	NL80211_CMD_GET_BEACON                                  = 0xd
	NL80211_CMD_GET_COALESCE                                = 0x64
	NL80211_CMD_GET_FTM_RESPONDER_STATS                     = 0x82
	NL80211_CMD_GET_INTERFACE                               = 0x5
	NL80211_CMD_GET_KEY                                     = 0x9
	NL80211_CMD_GET_MESH_CONFIG                             = 0x1c
	NL80211_CMD_GET_MESH_PARAMS                             = 0x1c
	NL80211_CMD_GET_MPATH                                   = 0x15
	NL80211_CMD_GET_MPP                                     = 0x6b
	NL80211_CMD_GET_POWER_SAVE                              = 0x3e
	NL80211_CMD_GET_PROTOCOL_FEATURES                       = 0x5f
	NL80211_CMD_GET_REG                                     = 0x1f
	NL80211_CMD_GET_SCAN                                    = 0x20
	NL80211_CMD_GET_STATION                                 = 0x11
	NL80211_CMD_GET_SURVEY                                  = 0x32
	NL80211_CMD_GET_WIPHY                                   = 0x1
	NL80211_CMD_GET_WOWLAN                                  = 0x49
	NL80211_CMD_JOIN_IBSS                                   = 0x2b
	NL80211_CMD_JOIN_MESH                                   = 0x44
	NL80211_CMD_JOIN_OCB                                    = 0x6c
	NL80211_CMD_LEAVE_IBSS                                  = 0x2c
	NL80211_CMD_LEAVE_MESH                                  = 0x45
	NL80211_CMD_LEAVE_OCB                                   = 0x6d
	NL80211_CMD_LINKS_REMOVED                               = 0x9a
	NL80211_CMD_MAX                                         = 0x9d
	NL80211_CMD_MICHAEL_MIC_FAILURE                         = 0x29
	NL80211_CMD_MODIFY_LINK_STA                             = 0x97
	NL80211_CMD_NAN_MATCH                                   = 0x78
	NL80211_CMD_NEW_BEACON                                  = 0xf
	NL80211_CMD_NEW_INTERFACE                               = 0x7
	NL80211_CMD_NEW_KEY                                     = 0xb
	NL80211_CMD_NEW_MPATH                                   = 0x17
	NL80211_CMD_NEW_PEER_CANDIDATE                          = 0x48
	NL80211_CMD_NEW_SCAN_RESULTS                            = 0x22
	NL80211_CMD_NEW_STATION                                 = 0x13
	NL80211_CMD_NEW_SURVEY_RESULTS                          = 0x33
	NL80211_CMD_NEW_WIPHY                                   = 0x3
	NL80211_CMD_NOTIFY_CQM                                  = 0x40
	NL80211_CMD_NOTIFY_RADAR                                = 0x86
	NL80211_CMD_OBSS_COLOR_COLLISION                        = 0x8d
	NL80211_CMD_PEER_MEASUREMENT_COMPLETE                   = 0x85
	NL80211_CMD_PEER_MEASUREMENT_RESULT                     = 0x84
	NL80211_CMD_PEER_MEASUREMENT_START                      = 0x83
	NL80211_CMD_PMKSA_CANDIDATE                             = 0x50
	NL80211_CMD_PORT_AUTHORIZED                             = 0x7d
	NL80211_CMD_PROBE_CLIENT                                = 0x54
	NL80211_CMD_PROBE_MESH_LINK                             = 0x88
	NL80211_CMD_RADAR_DETECT                                = 0x5e
	NL80211_CMD_REG_BEACON_HINT                             = 0x2a
	NL80211_CMD_REG_CHANGE                                  = 0x24
	NL80211_CMD_REGISTER_ACTION                             = 0x3a
	NL80211_CMD_REGISTER_BEACONS                            = 0x55
	NL80211_CMD_REGISTER_FRAME                              = 0x3a
	NL80211_CMD_RELOAD_REGDB                                = 0x7e
	NL80211_CMD_REMAIN_ON_CHANNEL                           = 0x37
	NL80211_CMD_REMOVE_LINK                                 = 0x95
	NL80211_CMD_REMOVE_LINK_STA                             = 0x98
	NL80211_CMD_REQ_SET_REG                                 = 0x1b
	NL80211_CMD_ROAM                                        = 0x2f
	NL80211_CMD_SCAN_ABORTED                                = 0x23
	NL80211_CMD_SCHED_SCAN_RESULTS                          = 0x4d
	NL80211_CMD_SCHED_SCAN_STOPPED                          = 0x4e
	NL80211_CMD_SET_BEACON                                  = 0xe
	NL80211_CMD_SET_BSS                                     = 0x19
	NL80211_CMD_SET_CHANNEL                                 = 0x41
	NL80211_CMD_SET_COALESCE                                = 0x65
	NL80211_CMD_SET_CQM                                     = 0x3f
	NL80211_CMD_SET_FILS_AAD                                = 0x92
	NL80211_CMD_SET_HW_TIMESTAMP                            = 0x99
	NL80211_CMD_SET_INTERFACE                               = 0x6
	NL80211_CMD_SET_KEY                                     = 0xa
	NL80211_CMD_SET_MAC_ACL                                 = 0x5d
	NL80211_CMD_SET_MCAST_RATE                              = 0x5c
	NL80211_CMD_SET_MESH_CONFIG                             = 0x1d
	NL80211_CMD_SET_MESH_PARAMS                             = 0x1d
	NL80211_CMD_SET_MGMT_EXTRA_IE                           = 0x1e
	NL80211_CMD_SET_MPATH                                   = 0x16
	NL80211_CMD_SET_MULTICAST_TO_UNICAST                    = 0x79
	NL80211_CMD_SET_NOACK_MAP                               = 0x57
	NL80211_CMD_SET_PMK                                     = 0x7b
	NL80211_CMD_SET_PMKSA                                   = 0x34
	NL80211_CMD_SET_POWER_SAVE                              = 0x3d
	NL80211_CMD_SET_QOS_MAP                                 = 0x68
	NL80211_CMD_SET_REG                                     = 0x1a
	NL80211_CMD_SET_REKEY_OFFLOAD                           = 0x4f
	NL80211_CMD_SET_SAR_SPECS                               = 0x8c
	NL80211_CMD_SET_STATION                                 = 0x12
	NL80211_CMD_SET_TID_CONFIG                              = 0x89
	NL80211_CMD_SET_TID_TO_LINK_MAPPING                     = 0x9b
	NL80211_CMD_SET_TX_BITRATE_MASK                         = 0x39
	NL80211_CMD_SET_WDS_PEER                                = 0x42
	NL80211_CMD_SET_WIPHY                                   = 0x2
	NL80211_CMD_SET_WIPHY_NETNS                             = 0x31
	NL80211_CMD_SET_WOWLAN                                  = 0x4a
	NL80211_CMD_STA_OPMODE_CHANGED                          = 0x80
	NL80211_CMD_START_AP                                    = 0xf
	NL80211_CMD_START_NAN                                   = 0x73
	NL80211_CMD_START_P2P_DEVICE                            = 0x59
	NL80211_CMD_START_SCHED_SCAN                            = 0x4b
	NL80211_CMD_STOP_AP                                     = 0x10
	NL80211_CMD_STOP_NAN                                    = 0x74
	NL80211_CMD_STOP_P2P_DEVICE                             = 0x5a
	NL80211_CMD_STOP_SCHED_SCAN                             = 0x4c
	NL80211_CMD_TDLS_CANCEL_CHANNEL_SWITCH                  = 0x70
	NL80211_CMD_TDLS_CHANNEL_SWITCH                         = 0x6f
	NL80211_CMD_TDLS_MGMT                                   = 0x52
	NL80211_CMD_TDLS_OPER                                   = 0x51
	NL80211_CMD_TESTMODE                                    = 0x2d
	NL80211_CMD_TRIGGER_SCAN                                = 0x21
	NL80211_CMD_UNEXPECTED_4ADDR_FRAME                      = 0x56
	NL80211_CMD_UNEXPECTED_FRAME                            = 0x53
	NL80211_CMD_UNPROT_BEACON                               = 0x8a
	NL80211_CMD_UNPROT_DEAUTHENTICATE                       = 0x46
	NL80211_CMD_UNPROT_DISASSOCIATE                         = 0x47
	NL80211_CMD_UNSPEC                                      = 0x0
	NL80211_CMD_UPDATE_CONNECT_PARAMS                       = 0x7a
	NL80211_CMD_UPDATE_FT_IES                               = 0x60
	NL80211_CMD_UPDATE_OWE_INFO                             = 0x87
	NL80211_CMD_VENDOR                                      = 0x67
	NL80211_CMD_WIPHY_REG_CHANGE                            = 0x71
	NL80211_COALESCE_CONDITION_MATCH                        = 0x0
	NL80211_COALESCE_CONDITION_NO_MATCH                     = 0x1
	NL80211_CONN_FAIL_BLOCKED_CLIENT                        = 0x1
	NL80211_CONN_FAIL_MAX_CLIENTS                           = 0x0
	NL80211_CQM_RSSI_BEACON_LOSS_EVENT                      = 0x2
	NL80211_CQM_RSSI_THRESHOLD_EVENT_HIGH                   = 0x1
	NL80211_CQM_RSSI_THRESHOLD_EVENT_LOW                    = 0x0
	NL80211_CQM_TXE_MAX_INTVL                               = 0x708
	NL80211_CRIT_PROTO_APIPA                                = 0x3
	NL80211_CRIT_PROTO_DHCP                                 = 0x1
	NL80211_CRIT_PROTO_EAPOL                                = 0x2
	NL80211_CRIT_PROTO_MAX_DURATION                         = 0x1388
	NL80211_CRIT_PROTO_UNSPEC                               = 0x0
	NL80211_DFS_AVAILABLE                                   = 0x2
	NL80211_DFS_ETSI                                        = 0x2
	NL80211_DFS_FCC                                         = 0x1
	NL80211_DFS_JP                                          = 0x3
	NL80211_DFS_UNAVAILABLE                                 = 0x1
	NL80211_DFS_UNSET                                       = 0x0
	NL80211_DFS_USABLE                                      = 0x0
	NL80211_EDMG_BW_CONFIG_MAX                              = 0xf
	NL80211_EDMG_BW_CONFIG_MIN                              = 0x4
	NL80211_EDMG_CHANNELS_MAX                               = 0x3c
	NL80211_EDMG_CHANNELS_MIN                               = 0x1
	NL80211_EHT_MAX_CAPABILITY_LEN                          = 0x33
	NL80211_EHT_MIN_CAPABILITY_LEN                          = 0xd
	NL80211_EXTERNAL_AUTH_ABORT                             = 0x1
	NL80211_EXTERNAL_AUTH_START                             = 0x0
	NL80211_EXT_FEATURE_4WAY_HANDSHAKE_AP_PSK               = 0x32
	NL80211_EXT_FEATURE_4WAY_HANDSHAKE_STA_1X               = 0x10
	NL80211_EXT_FEATURE_4WAY_HANDSHAKE_STA_PSK              = 0xf
	NL80211_EXT_FEATURE_ACCEPT_BCAST_PROBE_RESP             = 0x12
	NL80211_EXT_FEATURE_ACK_SIGNAL_SUPPORT                  = 0x1b
	NL80211_EXT_FEATURE_AIRTIME_FAIRNESS                    = 0x21
	NL80211_EXT_FEATURE_AP_PMKSA_CACHING                    = 0x22
	NL80211_EXT_FEATURE_AQL                                 = 0x28
	NL80211_EXT_FEATURE_AUTH_AND_DEAUTH_RANDOM_TA           = 0x40
	NL80211_EXT_FEATURE_BEACON_PROTECTION_CLIENT            = 0x2e
	NL80211_EXT_FEATURE_BEACON_PROTECTION                   = 0x29
	NL80211_EXT_FEATURE_BEACON_RATE_HE                      = 0x36
	NL80211_EXT_FEATURE_BEACON_RATE_HT                      = 0x7
	NL80211_EXT_FEATURE_BEACON_RATE_LEGACY                  = 0x6
	NL80211_EXT_FEATURE_BEACON_RATE_VHT                     = 0x8
	NL80211_EXT_FEATURE_BSS_COLOR                           = 0x3a
	NL80211_EXT_FEATURE_BSS_PARENT_TSF                      = 0x4
	NL80211_EXT_FEATURE_CAN_REPLACE_PTK0                    = 0x1f
	NL80211_EXT_FEATURE_CONTROL_PORT_NO_PREAUTH             = 0x2a
	NL80211_EXT_FEATURE_CONTROL_PORT_OVER_NL80211           = 0x1a
	NL80211_EXT_FEATURE_CONTROL_PORT_OVER_NL80211_TX_STATUS = 0x30
	NL80211_EXT_FEATURE_CQM_RSSI_LIST                       = 0xd
	NL80211_EXT_FEATURE_DATA_ACK_SIGNAL_SUPPORT             = 0x1b
	NL80211_EXT_FEATURE_DEL_IBSS_STA                        = 0x2c
	NL80211_EXT_FEATURE_DFS_CONCURRENT                      = 0x43
	NL80211_EXT_FEATURE_DFS_OFFLOAD                         = 0x19
	NL80211_EXT_FEATURE_ENABLE_FTM_RESPONDER                = 0x20
	NL80211_EXT_FEATURE_EXT_KEY_ID                          = 0x24
	NL80211_EXT_FEATURE_FILS_CRYPTO_OFFLOAD                 = 0x3b
	NL80211_EXT_FEATURE_FILS_DISCOVERY                      = 0x34
	NL80211_EXT_FEATURE_FILS_MAX_CHANNEL_TIME               = 0x11
	NL80211_EXT_FEATURE_FILS_SK_OFFLOAD                     = 0xe
	NL80211_EXT_FEATURE_FILS_STA                            = 0x9
	NL80211_EXT_FEATURE_HIGH_ACCURACY_SCAN                  = 0x18
	NL80211_EXT_FEATURE_LOW_POWER_SCAN                      = 0x17
	NL80211_EXT_FEATURE_LOW_SPAN_SCAN                       = 0x16
	NL80211_EXT_FEATURE_MFP_OPTIONAL                        = 0x15
	NL80211_EXT_FEATURE_MGMT_TX_RANDOM_TA                   = 0xa
	NL80211_EXT_FEATURE_MGMT_TX_RANDOM_TA_CONNECTED         = 0xb
	NL80211_EXT_FEATURE_MULTICAST_REGISTRATIONS             = 0x2d
	NL80211_EXT_FEATURE_MU_MIMO_AIR_SNIFFER                 = 0x2
	NL80211_EXT_FEATURE_OCE_PROBE_REQ_DEFERRAL_SUPPRESSION  = 0x14
	NL80211_EXT_FEATURE_OCE_PROBE_REQ_HIGH_TX_RATE          = 0x13
	NL80211_EXT_FEATURE_OPERATING_CHANNEL_VALIDATION        = 0x31
	NL80211_EXT_FEATURE_OWE_OFFLOAD_AP                      = 0x42
	NL80211_EXT_FEATURE_OWE_OFFLOAD                         = 0x41
	NL80211_EXT_FEATURE_POWERED_ADDR_CHANGE                 = 0x3d
	NL80211_EXT_FEATURE_PROTECTED_TWT                       = 0x2b
	NL80211_EXT_FEATURE_PROT_RANGE_NEGO_AND_MEASURE         = 0x39
	NL80211_EXT_FEATURE_PUNCT                               = 0x3e
	NL80211_EXT_FEATURE_RADAR_BACKGROUND                    = 0x3c
	NL80211_EXT_FEATURE_RRM                                 = 0x1
	NL80211_EXT_FEATURE_SAE_OFFLOAD_AP                      = 0x33
	NL80211_EXT_FEATURE_SAE_OFFLOAD                         = 0x26
	NL80211_EXT_FEATURE_SCAN_FREQ_KHZ                       = 0x2f
	NL80211_EXT_FEATURE_SCAN_MIN_PREQ_CONTENT               = 0x1e
	NL80211_EXT_FEATURE_SCAN_RANDOM_SN                      = 0x1d
	NL80211_EXT_FEATURE_SCAN_START_TIME                     = 0x3
	NL80211_EXT_FEATURE_SCHED_SCAN_BAND_SPECIFIC_RSSI_THOLD = 0x23
	NL80211_EXT_FEATURE_SCHED_SCAN_RELATIVE_RSSI            = 0xc
	NL80211_EXT_FEATURE_SECURE_LTF                          = 0x37
	NL80211_EXT_FEATURE_SECURE_NAN                          = 0x3f
	NL80211_EXT_FEATURE_SECURE_RTT                          = 0x38
	NL80211_EXT_FEATURE_SET_SCAN_DWELL                      = 0x5
	NL80211_EXT_FEATURE_SPP_AMSDU_SUPPORT                   = 0x44
	NL80211_EXT_FEATURE_STA_TX_PWR                          = 0x25
	NL80211_EXT_FEATURE_TXQS                                = 0x1c
	NL80211_EXT_FEATURE_UNSOL_BCAST_PROBE_RESP              = 0x35
	NL80211_EXT_FEATURE_VHT_IBSS                            = 0x0
	NL80211_EXT_FEATURE_VLAN_OFFLOAD                        = 0x27
	NL80211_FEATURE_ACKTO_ESTIMATION                        = 0x800000
	NL80211_FEATURE_ACTIVE_MONITOR                          = 0x20000
	NL80211_FEATURE_ADVERTISE_CHAN_LIMITS                   = 0x4000
	NL80211_FEATURE_AP_MODE_CHAN_WIDTH_CHANGE               = 0x40000
	NL80211_FEATURE_AP_SCAN                                 = 0x100
	NL80211_FEATURE_CELL_BASE_REG_HINTS                     = 0x8
	NL80211_FEATURE_DS_PARAM_SET_IE_IN_PROBES               = 0x80000
	NL80211_FEATURE_DYNAMIC_SMPS                            = 0x2000000
	NL80211_FEATURE_FULL_AP_CLIENT_STATE                    = 0x8000
	NL80211_FEATURE_HT_IBSS                                 = 0x2
	NL80211_FEATURE_INACTIVITY_TIMER                        = 0x4
	NL80211_FEATURE_LOW_PRIORITY_SCAN                       = 0x40
	NL80211_FEATURE_MAC_ON_CREATE                           = 0x8000000
	NL80211_FEATURE_ND_RANDOM_MAC_ADDR                      = 0x80000000
	NL80211_FEATURE_NEED_OBSS_SCAN                          = 0x400
	NL80211_FEATURE_P2P_DEVICE_NEEDS_CHANNEL                = 0x10
	NL80211_FEATURE_P2P_GO_CTWIN                            = 0x800
	NL80211_FEATURE_P2P_GO_OPPPS                            = 0x1000
	NL80211_FEATURE_QUIET                                   = 0x200000
	NL80211_FEATURE_SAE                                     = 0x20
	NL80211_FEATURE_SCAN_FLUSH                              = 0x80
	NL80211_FEATURE_SCAN_RANDOM_MAC_ADDR                    = 0x20000000
	NL80211_FEATURE_SCHED_SCAN_RANDOM_MAC_ADDR              = 0x40000000
	NL80211_FEATURE_SK_TX_STATUS                            = 0x1
	NL80211_FEATURE_STATIC_SMPS                             = 0x1000000
	NL80211_FEATURE_SUPPORTS_WMM_ADMISSION                  = 0x4000000
	NL80211_FEATURE_TDLS_CHANNEL_SWITCH                     = 0x10000000
	NL80211_FEATURE_TX_POWER_INSERTION                      = 0x400000
	NL80211_FEATURE_USERSPACE_MPM                           = 0x10000
	NL80211_FEATURE_VIF_TXPOWER                             = 0x200
	NL80211_FEATURE_WFA_TPC_IE_IN_PROBES                    = 0x100000
	NL80211_FILS_DISCOVERY_ATTR_INT_MAX                     = 0x2
	NL80211_FILS_DISCOVERY_ATTR_INT_MIN                     = 0x1
	NL80211_FILS_DISCOVERY_ATTR_MAX                         = 0x3
	NL80211_FILS_DISCOVERY_ATTR_TMPL                        = 0x3
	NL80211_FILS_DISCOVERY_TMPL_MIN_LEN                     = 0x2a
	NL80211_FREQUENCY_ATTR_16MHZ                            = 0x19
	NL80211_FREQUENCY_ATTR_1MHZ                             = 0x15
	NL80211_FREQUENCY_ATTR_2MHZ                             = 0x16
	NL80211_FREQUENCY_ATTR_4MHZ                             = 0x17
	NL80211_FREQUENCY_ATTR_8MHZ                             = 0x18
	NL80211_FREQUENCY_ATTR_ALLOW_6GHZ_VLP_AP                = 0x21
	NL80211_FREQUENCY_ATTR_CAN_MONITOR                      = 0x20
	NL80211_FREQUENCY_ATTR_DFS_CAC_TIME                     = 0xd
	NL80211_FREQUENCY_ATTR_DFS_CONCURRENT                   = 0x1d
	NL80211_FREQUENCY_ATTR_DFS_STATE                        = 0x7
	NL80211_FREQUENCY_ATTR_DFS_TIME                         = 0x8
	NL80211_FREQUENCY_ATTR_DISABLED                         = 0x2
	NL80211_FREQUENCY_ATTR_FREQ                             = 0x1
	NL80211_FREQUENCY_ATTR_GO_CONCURRENT                    = 0xf
	NL80211_FREQUENCY_ATTR_INDOOR_ONLY                      = 0xe
	NL80211_FREQUENCY_ATTR_IR_CONCURRENT                    = 0xf
	NL80211_FREQUENCY_ATTR_MAX                              = 0x22
	NL80211_FREQUENCY_ATTR_MAX_TX_POWER                     = 0x6
	NL80211_FREQUENCY_ATTR_NO_10MHZ                         = 0x11
	NL80211_FREQUENCY_ATTR_NO_160MHZ                        = 0xc
	NL80211_FREQUENCY_ATTR_NO_20MHZ                         = 0x10
	NL80211_FREQUENCY_ATTR_NO_320MHZ                        = 0x1a
	NL80211_FREQUENCY_ATTR_NO_6GHZ_AFC_CLIENT               = 0x1f
	NL80211_FREQUENCY_ATTR_NO_6GHZ_VLP_CLIENT               = 0x1e
	NL80211_FREQUENCY_ATTR_NO_80MHZ                         = 0xb
	NL80211_FREQUENCY_ATTR_NO_EHT                           = 0x1b
	NL80211_FREQUENCY_ATTR_NO_HE                            = 0x13
	NL80211_FREQUENCY_ATTR_NO_HT40_MINUS                    = 0x9
	NL80211_FREQUENCY_ATTR_NO_HT40_PLUS                     = 0xa
	NL80211_FREQUENCY_ATTR_NO_IBSS                          = 0x3
	NL80211_FREQUENCY_ATTR_NO_IR                            = 0x3
	NL80211_FREQUENCY_ATTR_NO_UHB_AFC_CLIENT                = 0x1f
	NL80211_FREQUENCY_ATTR_NO_UHB_VLP_CLIENT                = 0x1e
	NL80211_FREQUENCY_ATTR_OFFSET                           = 0x14
	NL80211_FREQUENCY_ATTR_PASSIVE_SCAN                     = 0x3
	NL80211_FREQUENCY_ATTR_PSD                              = 0x1c
	NL80211_FREQUENCY_ATTR_RADAR                            = 0x5
	NL80211_FREQUENCY_ATTR_WMM                              = 0x12
	NL80211_FTM_RESP_ATTR_CIVICLOC                          = 0x3
	NL80211_FTM_RESP_ATTR_ENABLED                           = 0x1
	NL80211_FTM_RESP_ATTR_LCI                               = 0x2
	NL80211_FTM_RESP_ATTR_MAX                               = 0x3
	NL80211_FTM_STATS_ASAP_NUM                              = 0x4
	NL80211_FTM_STATS_FAILED_NUM                            = 0x3
	NL80211_FTM_STATS_MAX                                   = 0xa
	NL80211_FTM_STATS_NON_ASAP_NUM                          = 0x5
	NL80211_FTM_STATS_OUT_OF_WINDOW_TRIGGERS_NUM            = 0x9
	NL80211_FTM_STATS_PAD                                   = 0xa
	NL80211_FTM_STATS_PARTIAL_NUM                           = 0x2
	NL80211_FTM_STATS_RESCHEDULE_REQUESTS_NUM               = 0x8
	NL80211_FTM_STATS_SUCCESS_NUM                           = 0x1
	NL80211_FTM_STATS_TOTAL_DURATION_MSEC                   = 0x6
	NL80211_FTM_STATS_UNKNOWN_TRIGGERS_NUM                  = 0x7
	NL80211_GENL_NAME                                       = "nl80211"
	NL80211_HE_BSS_COLOR_ATTR_COLOR                         = 0x1
	NL80211_HE_BSS_COLOR_ATTR_DISABLED                      = 0x2
	NL80211_HE_BSS_COLOR_ATTR_MAX                           = 0x3
	NL80211_HE_BSS_COLOR_ATTR_PARTIAL                       = 0x3
	NL80211_HE_MAX_CAPABILITY_LEN                           = 0x36
	NL80211_HE_MIN_CAPABILITY_LEN                           = 0x10
	NL80211_HE_NSS_MAX                                      = 0x8
	NL80211_HE_OBSS_PD_ATTR_BSS_COLOR_BITMAP                = 0x4
	NL80211_HE_OBSS_PD_ATTR_MAX                             = 0x6
	NL80211_HE_OBSS_PD_ATTR_MAX_OFFSET                      = 0x2
	NL80211_HE_OBSS_PD_ATTR_MIN_OFFSET                      = 0x1
	NL80211_HE_OBSS_PD_ATTR_NON_SRG_MAX_OFFSET              = 0x3
	NL80211_HE_OBSS_PD_ATTR_PARTIAL_BSSID_BITMAP            = 0x5
	NL80211_HE_OBSS_PD_ATTR_SR_CTRL                         = 0x6
	NL80211_HIDDEN_SSID_NOT_IN_USE                          = 0x0
	NL80211_HIDDEN_SSID_ZERO_CONTENTS                       = 0x2
	NL80211_HIDDEN_SSID_ZERO_LEN                            = 0x1
	NL80211_HT_CAPABILITY_LEN                               = 0x1a
	NL80211_IFACE_COMB_BI_MIN_GCD                           = 0x7
	NL80211_IFACE_COMB_LIMITS                               = 0x1
	NL80211_IFACE_COMB_MAXNUM                               = 0x2
	NL80211_IFACE_COMB_NUM_CHANNELS                         = 0x4
	NL80211_IFACE_COMB_RADAR_DETECT_REGIONS                 = 0x6
	NL80211_IFACE_COMB_RADAR_DETECT_WIDTHS                  = 0x5
	NL80211_IFACE_COMB_STA_AP_BI_MATCH                      = 0x3
	NL80211_IFACE_COMB_UNSPEC                               = 0x0
	NL80211_IFACE_LIMIT_MAX                                 = 0x1
	NL80211_IFACE_LIMIT_TYPES                               = 0x2
	NL80211_IFACE_LIMIT_UNSPEC                              = 0x0
	NL80211_IFTYPE_ADHOC                                    = 0x1
	NL80211_IFTYPE_AKM_ATTR_IFTYPES                         = 0x1
	NL80211_IFTYPE_AKM_ATTR_MAX                             = 0x2
	NL80211_IFTYPE_AKM_ATTR_SUITES                          = 0x2
	NL80211_IFTYPE_AP                                       = 0x3
	NL80211_IFTYPE_AP_VLAN                                  = 0x4
	NL80211_IFTYPE_MAX                                      = 0xc
	NL80211_IFTYPE_MESH_POINT                               = 0x7
	NL80211_IFTYPE_MONITOR                                  = 0x6
	NL80211_IFTYPE_NAN                                      = 0xc
	NL80211_IFTYPE_OCB                                      = 0xb
	NL80211_IFTYPE_P2P_CLIENT                               = 0x8
	NL80211_IFTYPE_P2P_DEVICE                               = 0xa
	NL80211_IFTYPE_P2P_GO                                   = 0x9
	NL80211_IFTYPE_STATION                                  = 0x2
	NL80211_IFTYPE_UNSPECIFIED                              = 0x0
	NL80211_IFTYPE_WDS                                      = 0x5
	NL80211_KCK_EXT_LEN_32                                  = 0x20
	NL80211_KCK_EXT_LEN                                     = 0x18
	NL80211_KCK_LEN                                         = 0x10
	NL80211_KEK_EXT_LEN                                     = 0x20
	NL80211_KEK_LEN                                         = 0x10
	NL80211_KEY_CIPHER                                      = 0x3
	NL80211_KEY_DATA                                        = 0x1
	NL80211_KEY_DEFAULT_BEACON                              = 0xa
	NL80211_KEY_DEFAULT                                     = 0x5
	NL80211_KEY_DEFAULT_MGMT                                = 0x6
	NL80211_KEY_DEFAULT_TYPE_MULTICAST                      = 0x2
	NL80211_KEY_DEFAULT_TYPES                               = 0x8
	NL80211_KEY_DEFAULT_TYPE_UNICAST                        = 0x1
	NL80211_KEY_IDX                                         = 0x2
	NL80211_KEY_MAX                                         = 0xa
	NL80211_KEY_MODE                                        = 0x9
	NL80211_KEY_NO_TX                                       = 0x1
	NL80211_KEY_RX_TX                                       = 0x0
	NL80211_KEY_SEQ                                         = 0x4
	NL80211_KEY_SET_TX                                      = 0x2
	NL80211_KEY_TYPE                                        = 0x7
	NL80211_KEYTYPE_GROUP                                   = 0x0
	NL80211_KEYTYPE_PAIRWISE                                = 0x1
	NL80211_KEYTYPE_PEERKEY                                 = 0x2
	NL80211_MAX_NR_AKM_SUITES                               = 0x2
	NL80211_MAX_NR_CIPHER_SUITES                            = 0x5
	NL80211_MAX_SUPP_HT_RATES                               = 0x4d
	NL80211_MAX_SUPP_RATES                                  = 0x20
	NL80211_MAX_SUPP_REG_RULES                              = 0x80
	NL80211_MAX_SUPP_SELECTORS                              = 0x80
	NL80211_MBSSID_CONFIG_ATTR_EMA                          = 0x5
	NL80211_MBSSID_CONFIG_ATTR_INDEX                        = 0x3
	NL80211_MBSSID_CONFIG_ATTR_MAX                          = 0x6
	NL80211_MBSSID_CONFIG_ATTR_MAX_EMA_PROFILE_PERIODICITY  = 0x2
	NL80211_MBSSID_CONFIG_ATTR_MAX_INTERFACES               = 0x1
	NL80211_MBSSID_CONFIG_ATTR_TX_IFINDEX                   = 0x4
	NL80211_MESHCONF_ATTR_MAX                               = 0x1f
	NL80211_MESHCONF_AUTO_OPEN_PLINKS                       = 0x7
	NL80211_MESHCONF_AWAKE_WINDOW                           = 0x1b
	NL80211_MESHCONF_CONFIRM_TIMEOUT                        = 0x2
	NL80211_MESHCONF_CONNECTED_TO_AS                        = 0x1f
	NL80211_MESHCONF_CONNECTED_TO_GATE                      = 0x1d
	NL80211_MESHCONF_ELEMENT_TTL                            = 0xf
	NL80211_MESHCONF_FORWARDING                             = 0x13
	NL80211_MESHCONF_GATE_ANNOUNCEMENTS                     = 0x11
	NL80211_MESHCONF_HOLDING_TIMEOUT                        = 0x3
	NL80211_MESHCONF_HT_OPMODE                              = 0x16
	NL80211_MESHCONF_HWMP_ACTIVE_PATH_TIMEOUT               = 0xb
	NL80211_MESHCONF_HWMP_CONFIRMATION_INTERVAL             = 0x19
	NL80211_MESHCONF_HWMP_MAX_PREQ_RETRIES                  = 0x8
	NL80211_MESHCONF_HWMP_NET_DIAM_TRVS_TIME                = 0xd
	NL80211_MESHCONF_HWMP_PATH_TO_ROOT_TIMEOUT              = 0x17
	NL80211_MESHCONF_HWMP_PERR_MIN_INTERVAL                 = 0x12
	NL80211_MESHCONF_HWMP_PREQ_MIN_INTERVAL                 = 0xc
	NL80211_MESHCONF_HWMP_RANN_INTERVAL                     = 0x10
	NL80211_MESHCONF_HWMP_ROOT_INTERVAL                     = 0x18
	NL80211_MESHCONF_HWMP_ROOTMODE                          = 0xe
	NL80211_MESHCONF_MAX_PEER_LINKS                         = 0x4
	NL80211_MESHCONF_MAX_RETRIES                            = 0x5
	NL80211_MESHCONF_MIN_DISCOVERY_TIMEOUT                  = 0xa
	NL80211_MESHCONF_NOLEARN                                = 0x1e
	NL80211_MESHCONF_PATH_REFRESH_TIME                      = 0x9
	NL80211_MESHCONF_PLINK_TIMEOUT                          = 0x1c
	NL80211_MESHCONF_POWER_MODE                             = 0x1a
	NL80211_MESHCONF_RETRY_TIMEOUT                          = 0x1
	NL80211_MESHCONF_RSSI_THRESHOLD                         = 0x14
	NL80211_MESHCONF_SYNC_OFFSET_MAX_NEIGHBOR               = 0x15
	NL80211_MESHCONF_TTL                                    = 0x6
	NL80211_MESH_POWER_ACTIVE                               = 0x1
	NL80211_MESH_POWER_DEEP_SLEEP                           = 0x3
	NL80211_MESH_POWER_LIGHT_SLEEP                          = 0x2
	NL80211_MESH_POWER_MAX                                  = 0x3
	NL80211_MESH_POWER_UNKNOWN                              = 0x0
	NL80211_MESH_SETUP_ATTR_MAX                             = 0x8
	NL80211_MESH_SETUP_AUTH_PROTOCOL                        = 0x8
	NL80211_MESH_SETUP_ENABLE_VENDOR_METRIC                 = 0x2
	NL80211_MESH_SETUP_ENABLE_VENDOR_PATH_SEL               = 0x1
	NL80211_MESH_SETUP_ENABLE_VENDOR_SYNC                   = 0x6
	NL80211_MESH_SETUP_IE                                   = 0x3
	NL80211_MESH_SETUP_USERSPACE_AMPE                       = 0x5
	NL80211_MESH_SETUP_USERSPACE_AUTH                       = 0x4
	NL80211_MESH_SETUP_USERSPACE_MPM                        = 0x7
	NL80211_MESH_SETUP_VENDOR_PATH_SEL_IE                   = 0x3
	NL80211_MFP_NO                                          = 0x0
	NL80211_MFP_OPTIONAL                                    = 0x2
	NL80211_MFP_REQUIRED                                    = 0x1
	NL80211_MIN_REMAIN_ON_CHANNEL_TIME                      = 0xa
	NL80211_MNTR_FLAG_ACTIVE                                = 0x6
	NL80211_MNTR_FLAG_CONTROL                               = 0x3
	NL80211_MNTR_FLAG_COOK_FRAMES                           = 0x5
	NL80211_MNTR_FLAG_FCSFAIL                               = 0x1
	NL80211_MNTR_FLAG_MAX                                   = 0x7
	NL80211_MNTR_FLAG_OTHER_BSS                             = 0x4
	NL80211_MNTR_FLAG_PLCPFAIL                              = 0x2
	NL80211_MPATH_FLAG_ACTIVE                               = 0x1
	NL80211_MPATH_FLAG_FIXED                                = 0x8
	NL80211_MPATH_FLAG_RESOLVED                             = 0x10
	NL80211_MPATH_FLAG_RESOLVING                            = 0x2
	NL80211_MPATH_FLAG_SN_VALID                             = 0x4
	NL80211_MPATH_INFO_DISCOVERY_RETRIES                    = 0x7
	NL80211_MPATH_INFO_DISCOVERY_TIMEOUT                    = 0x6
	NL80211_MPATH_INFO_EXPTIME                              = 0x4
	NL80211_MPATH_INFO_FLAGS                                = 0x5
	NL80211_MPATH_INFO_FRAME_QLEN                           = 0x1
	NL80211_MPATH_INFO_HOP_COUNT                            = 0x8
	NL80211_MPATH_INFO_MAX                                  = 0x9
	NL80211_MPATH_INFO_METRIC                               = 0x3
	NL80211_MPATH_INFO_PATH_CHANGE                          = 0x9
	NL80211_MPATH_INFO_SN                                   = 0x2
	NL80211_MULTICAST_GROUP_CONFIG                          = "config"
	NL80211_MULTICAST_GROUP_MLME                            = "mlme"
	NL80211_MULTICAST_GROUP_NAN                             = "nan"
	NL80211_MULTICAST_GROUP_REG                             = "regulatory"
	NL80211_MULTICAST_GROUP_SCAN                            = "scan"
	NL80211_MULTICAST_GROUP_TESTMODE                        = "testmode"
	NL80211_MULTICAST_GROUP_VENDOR                          = "vendor"
	NL80211_NAN_FUNC_ATTR_MAX                               = 0x10
	NL80211_NAN_FUNC_CLOSE_RANGE                            = 0x9
	NL80211_NAN_FUNC_FOLLOW_UP                              = 0x2
	NL80211_NAN_FUNC_FOLLOW_UP_DEST                         = 0x8
	NL80211_NAN_FUNC_FOLLOW_UP_ID                           = 0x6
	NL80211_NAN_FUNC_FOLLOW_UP_REQ_ID                       = 0x7
	NL80211_NAN_FUNC_INSTANCE_ID                            = 0xf
	NL80211_NAN_FUNC_MAX_TYPE                               = 0x2
	NL80211_NAN_FUNC_PUBLISH_BCAST                          = 0x4
	NL80211_NAN_FUNC_PUBLISH                                = 0x0
	NL80211_NAN_FUNC_PUBLISH_TYPE                           = 0x3
	NL80211_NAN_FUNC_RX_MATCH_FILTER                        = 0xd
	NL80211_NAN_FUNC_SERVICE_ID                             = 0x2
	NL80211_NAN_FUNC_SERVICE_ID_LEN                         = 0x6
	NL80211_NAN_FUNC_SERVICE_INFO                           = 0xb
	NL80211_NAN_FUNC_SERVICE_SPEC_INFO_MAX_LEN              = 0xff
	NL80211_NAN_FUNC_SRF                                    = 0xc
	NL80211_NAN_FUNC_SRF_MAX_LEN                            = 0xff
	NL80211_NAN_FUNC_SUBSCRIBE_ACTIVE                       = 0x5
	NL80211_NAN_FUNC_SUBSCRIBE                              = 0x1
	NL80211_NAN_FUNC_TERM_REASON                            = 0x10
	NL80211_NAN_FUNC_TERM_REASON_ERROR                      = 0x2
	NL80211_NAN_FUNC_TERM_REASON_TTL_EXPIRED                = 0x1
	NL80211_NAN_FUNC_TERM_REASON_USER_REQUEST               = 0x0
	NL80211_NAN_FUNC_TTL                                    = 0xa
	NL80211_NAN_FUNC_TX_MATCH_FILTER                        = 0xe
	NL80211_NAN_FUNC_TYPE                                   = 0x1
	NL80211_NAN_MATCH_ATTR_MAX                              = 0x2
	NL80211_NAN_MATCH_FUNC_LOCAL                            = 0x1
	NL80211_NAN_MATCH_FUNC_PEER                             = 0x2
	NL80211_NAN_SOLICITED_PUBLISH                           = 0x1
	NL80211_NAN_SRF_ATTR_MAX                                = 0x4
	NL80211_NAN_SRF_BF                                      = 0x2
	NL80211_NAN_SRF_BF_IDX                                  = 0x3
	NL80211_NAN_SRF_INCLUDE                                 = 0x1
	NL80211_NAN_SRF_MAC_ADDRS                               = 0x4
	NL80211_NAN_UNSOLICITED_PUBLISH                         = 0x2
	NL80211_NUM_ACS                                         = 0x4
	NL80211_P2P_PS_SUPPORTED                                = 0x1
	NL80211_P2P_PS_UNSUPPORTED                              = 0x0
	NL80211_PKTPAT_MASK                                     = 0x1
	NL80211_PKTPAT_OFFSET                                   = 0x3
	NL80211_PKTPAT_PATTERN                                  = 0x2
	NL80211_PLINK_ACTION_BLOCK                              = 0x2
	NL80211_PLINK_ACTION_NO_ACTION                          = 0x0
	NL80211_PLINK_ACTION_OPEN                               = 0x1
	NL80211_PLINK_BLOCKED                                   = 0x6
	NL80211_PLINK_CNF_RCVD                                  = 0x3
	NL80211_PLINK_ESTAB                                     = 0x4
	NL80211_PLINK_HOLDING                                   = 0x5
	NL80211_PLINK_LISTEN                                    = 0x0
	NL80211_PLINK_OPN_RCVD                                  = 0x2
	NL80211_PLINK_OPN_SNT                                   = 0x1
	NL80211_PMKSA_CANDIDATE_BSSID                           = 0x2
	NL80211_PMKSA_CANDIDATE_INDEX                           = 0x1
	NL80211_PMKSA_CANDIDATE_PREAUTH                         = 0x3
	NL80211_PMSR_ATTR_MAX                                   = 0x5
	NL80211_PMSR_ATTR_MAX_PEERS                             = 0x1
	NL80211_PMSR_ATTR_PEERS                                 = 0x5
	NL80211_PMSR_ATTR_RANDOMIZE_MAC_ADDR                    = 0x3
	NL80211_PMSR_ATTR_REPORT_AP_TSF                         = 0x2
	NL80211_PMSR_ATTR_TYPE_CAPA                             = 0x4
	NL80211_PMSR_FTM_CAPA_ATTR_ASAP                         = 0x1
	NL80211_PMSR_FTM_CAPA_ATTR_BANDWIDTHS                   = 0x6
	NL80211_PMSR_FTM_CAPA_ATTR_MAX_BURSTS_EXPONENT          = 0x7
	NL80211_PMSR_FTM_CAPA_ATTR_MAX                          = 0xa
	NL80211_PMSR_FTM_CAPA_ATTR_MAX_FTMS_PER_BURST           = 0x8
	NL80211_PMSR_FTM_CAPA_ATTR_NON_ASAP                     = 0x2
	NL80211_PMSR_FTM_CAPA_ATTR_NON_TRIGGER_BASED            = 0xa
	NL80211_PMSR_FTM_CAPA_ATTR_PREAMBLES                    = 0x5
	NL80211_PMSR_FTM_CAPA_ATTR_REQ_CIVICLOC                 = 0x4
	NL80211_PMSR_FTM_CAPA_ATTR_REQ_LCI                      = 0x3
	NL80211_PMSR_FTM_CAPA_ATTR_TRIGGER_BASED                = 0x9
	NL80211_PMSR_FTM_FAILURE_BAD_CHANGED_PARAMS             = 0x7
	NL80211_PMSR_FTM_FAILURE_INVALID_TIMESTAMP              = 0x5
	NL80211_PMSR_FTM_FAILURE_NO_RESPONSE                    = 0x1
	NL80211_PMSR_FTM_FAILURE_PEER_BUSY                      = 0x6
	NL80211_PMSR_FTM_FAILURE_PEER_NOT_CAPABLE               = 0x4
	NL80211_PMSR_FTM_FAILURE_REJECTED                       = 0x2
	NL80211_PMSR_FTM_FAILURE_UNSPECIFIED                    = 0x0
	NL80211_PMSR_FTM_FAILURE_WRONG_CHANNEL                  = 0x3
	NL80211_PMSR_FTM_REQ_ATTR_ASAP                          = 0x1
	NL80211_PMSR_FTM_REQ_ATTR_BSS_COLOR                     = 0xd
	NL80211_PMSR_FTM_REQ_ATTR_BURST_DURATION                = 0x5
	NL80211_PMSR_FTM_REQ_ATTR_BURST_PERIOD                  = 0x4
	NL80211_PMSR_FTM_REQ_ATTR_FTMS_PER_BURST                = 0x6
	NL80211_PMSR_FTM_REQ_ATTR_LMR_FEEDBACK                  = 0xc
	NL80211_PMSR_FTM_REQ_ATTR_MAX                           = 0xd
	NL80211_PMSR_FTM_REQ_ATTR_NON_TRIGGER_BASED             = 0xb
	NL80211_PMSR_FTM_REQ_ATTR_NUM_BURSTS_EXP                = 0x3
	NL80211_PMSR_FTM_REQ_ATTR_NUM_FTMR_RETRIES              = 0x7
	NL80211_PMSR_FTM_REQ_ATTR_PREAMBLE                      = 0x2
	NL80211_PMSR_FTM_REQ_ATTR_REQUEST_CIVICLOC              = 0x9
	NL80211_PMSR_FTM_REQ_ATTR_REQUEST_LCI                   = 0x8
	NL80211_PMSR_FTM_REQ_ATTR_TRIGGER_BASED                 = 0xa
	NL80211_PMSR_FTM_RESP_ATTR_BURST_DURATION               = 0x7
	NL80211_PMSR_FTM_RESP_ATTR_BURST_INDEX                  = 0x2
	NL80211_PMSR_FTM_RESP_ATTR_BUSY_RETRY_TIME              = 0x5
	NL80211_PMSR_FTM_RESP_ATTR_CIVICLOC                     = 0x14
	NL80211_PMSR_FTM_RESP_ATTR_DIST_AVG                     = 0x10
	NL80211_PMSR_FTM_RESP_ATTR_DIST_SPREAD                  = 0x12
	NL80211_PMSR_FTM_RESP_ATTR_DIST_VARIANCE                = 0x11
	NL80211_PMSR_FTM_RESP_ATTR_FAIL_REASON                  = 0x1
	NL80211_PMSR_FTM_RESP_ATTR_FTMS_PER_BURST               = 0x8
	NL80211_PMSR_FTM_RESP_ATTR_LCI                          = 0x13
	NL80211_PMSR_FTM_RESP_ATTR_MAX                          = 0x15
	NL80211_PMSR_FTM_RESP_ATTR_NUM_BURSTS_EXP               = 0x6
	NL80211_PMSR_FTM_RESP_ATTR_NUM_FTMR_ATTEMPTS            = 0x3
	NL80211_PMSR_FTM_RESP_ATTR_NUM_FTMR_SUCCESSES           = 0x4
	NL80211_PMSR_FTM_RESP_ATTR_PAD                          = 0x15
	NL80211_PMSR_FTM_RESP_ATTR_RSSI_AVG                     = 0x9
	NL80211_PMSR_FTM_RESP_ATTR_RSSI_SPREAD                  = 0xa
	NL80211_PMSR_FTM_RESP_ATTR_RTT_AVG                      = 0xd
	NL80211_PMSR_FTM_RESP_ATTR_RTT_SPREAD                   = 0xf
	NL80211_PMSR_FTM_RESP_ATTR_RTT_VARIANCE                 = 0xe
	NL80211_PMSR_FTM_RESP_ATTR_RX_RATE                      = 0xc
	NL80211_PMSR_FTM_RESP_ATTR_TX_RATE                      = 0xb
	NL80211_PMSR_PEER_ATTR_ADDR                             = 0x1
	NL80211_PMSR_PEER_ATTR_CHAN                             = 0x2
	NL80211_PMSR_PEER_ATTR_MAX                              = 0x4
	NL80211_PMSR_PEER_ATTR_REQ                              = 0x3
	NL80211_PMSR_PEER_ATTR_RESP                             = 0x4
	NL80211_PMSR_REQ_ATTR_DATA                              = 0x1
	NL80211_PMSR_REQ_ATTR_GET_AP_TSF                        = 0x2
	NL80211_PMSR_REQ_ATTR_MAX                               = 0x2
	NL80211_PMSR_RESP_ATTR_AP_TSF                           = 0x4
	NL80211_PMSR_RESP_ATTR_DATA                             = 0x1
	NL80211_PMSR_RESP_ATTR_FINAL                            = 0x5
	NL80211_PMSR_RESP_ATTR_HOST_TIME                        = 0x3
	NL80211_PMSR_RESP_ATTR_MAX                              = 0x6
	NL80211_PMSR_RESP_ATTR_PAD                              = 0x6
	NL80211_PMSR_RESP_ATTR_STATUS                           = 0x2
	NL80211_PMSR_STATUS_FAILURE                             = 0x3
	NL80211_PMSR_STATUS_REFUSED                             = 0x1
	NL80211_PMSR_STATUS_SUCCESS                             = 0x0
	NL80211_PMSR_STATUS_TIMEOUT                             = 0x2
	NL80211_PMSR_TYPE_FTM                                   = 0x1
	NL80211_PMSR_TYPE_INVALID                               = 0x0
	NL80211_PMSR_TYPE_MAX                                   = 0x1
	NL80211_PREAMBLE_DMG                                    = 0x3
	NL80211_PREAMBLE_HE                                     = 0x4
	NL80211_PREAMBLE_HT                                     = 0x1
	NL80211_PREAMBLE_LEGACY                                 = 0x0
	NL80211_PREAMBLE_VHT                                    = 0x2
	NL80211_PROBE_RESP_OFFLOAD_SUPPORT_80211U               = 0x8
	NL80211_PROBE_RESP_OFFLOAD_SUPPORT_P2P                  = 0x4
	NL80211_PROBE_RESP_OFFLOAD_SUPPORT_WPS2                 = 0x2
	NL80211_PROBE_RESP_OFFLOAD_SUPPORT_WPS                  = 0x1
	NL80211_PROTOCOL_FEATURE_SPLIT_WIPHY_DUMP               = 0x1
	NL80211_PS_DISABLED                                     = 0x0
	NL80211_PS_ENABLED                                      = 0x1
	NL80211_RADAR_CAC_ABORTED                               = 0x2
	NL80211_RADAR_CAC_FINISHED                              = 0x1
	NL80211_RADAR_CAC_STARTED                               = 0x5
	NL80211_RADAR_DETECTED                                  = 0x0
	NL80211_RADAR_NOP_FINISHED                              = 0x3
	NL80211_RADAR_PRE_CAC_EXPIRED                           = 0x4
	NL80211_RATE_INFO_10_MHZ_WIDTH                          = 0xb
	NL80211_RATE_INFO_160_MHZ_WIDTH                         = 0xa
	NL80211_RATE_INFO_16_MHZ_WIDTH                          = 0x1d
	NL80211_RATE_INFO_1_MHZ_WIDTH                           = 0x19
	NL80211_RATE_INFO_2_MHZ_WIDTH                           = 0x1a
	NL80211_RATE_INFO_320_MHZ_WIDTH                         = 0x12
	NL80211_RATE_INFO_40_MHZ_WIDTH                          = 0x3
	NL80211_RATE_INFO_4_MHZ_WIDTH                           = 0x1b
	NL80211_RATE_INFO_5_MHZ_WIDTH                           = 0xc
	NL80211_RATE_INFO_80_MHZ_WIDTH                          = 0x8
	NL80211_RATE_INFO_80P80_MHZ_WIDTH                       = 0x9
	NL80211_RATE_INFO_8_MHZ_WIDTH                           = 0x1c
	NL80211_RATE_INFO_BITRATE32                             = 0x5
	NL80211_RATE_INFO_BITRATE                               = 0x1
	NL80211_RATE_INFO_EHT_GI_0_8                            = 0x0
	NL80211_RATE_INFO_EHT_GI_1_6                            = 0x1
	NL80211_RATE_INFO_EHT_GI_3_2                            = 0x2
	NL80211_RATE_INFO_EHT_GI                                = 0x15
	NL80211_RATE_INFO_EHT_MCS                               = 0x13
	NL80211_RATE_INFO_EHT_NSS                               = 0x14
	NL80211_RATE_INFO_EHT_RU_ALLOC_106                      = 0x3
	NL80211_RATE_INFO_EHT_RU_ALLOC_106P26                   = 0x4
	NL80211_RATE_INFO_EHT_RU_ALLOC_242                      = 0x5
	NL80211_RATE_INFO_EHT_RU_ALLOC_26                       = 0x0
	NL80211_RATE_INFO_EHT_RU_ALLOC_2x996                    = 0xb
	NL80211_RATE_INFO_EHT_RU_ALLOC_2x996P484                = 0xc
	NL80211_RATE_INFO_EHT_RU_ALLOC_3x996                    = 0xd
	NL80211_RATE_INFO_EHT_RU_ALLOC_3x996P484                = 0xe
	NL80211_RATE_INFO_EHT_RU_ALLOC_484                      = 0x6
	NL80211_RATE_INFO_EHT_RU_ALLOC_484P242                  = 0x7
	NL80211_RATE_INFO_EHT_RU_ALLOC_4x996                    = 0xf
	NL80211_RATE_INFO_EHT_RU_ALLOC_52                       = 0x1
	NL80211_RATE_INFO_EHT_RU_ALLOC_52P26                    = 0x2
	NL80211_RATE_INFO_EHT_RU_ALLOC_996                      = 0x8
	NL80211_RATE_INFO_EHT_RU_ALLOC_996P484                  = 0x9
	NL80211_RATE_INFO_EHT_RU_ALLOC_996P484P242              = 0xa
	NL80211_RATE_INFO_EHT_RU_ALLOC                          = 0x16
	NL80211_RATE_INFO_HE_1XLTF                              = 0x0
	NL80211_RATE_INFO_HE_2XLTF                              = 0x1
	NL80211_RATE_INFO_HE_4XLTF                              = 0x2
	NL80211_RATE_INFO_HE_DCM                                = 0x10
	NL80211_RATE_INFO_HE_GI_0_8                             = 0x0
	NL80211_RATE_INFO_HE_GI_1_6                             = 0x1
	NL80211_RATE_INFO_HE_GI_3_2                             = 0x2
	NL80211_RATE_INFO_HE_GI                                 = 0xf
	NL80211_RATE_INFO_HE_MCS                                = 0xd
	NL80211_RATE_INFO_HE_NSS                                = 0xe
	NL80211_RATE_INFO_HE_RU_ALLOC_106                       = 0x2
	NL80211_RATE_INFO_HE_RU_ALLOC_242                       = 0x3
	NL80211_RATE_INFO_HE_RU_ALLOC_26                        = 0x0
	NL80211_RATE_INFO_HE_RU_ALLOC_2x996                     = 0x6
	NL80211_RATE_INFO_HE_RU_ALLOC_484                       = 0x4
	NL80211_RATE_INFO_HE_RU_ALLOC_52                        = 0x1
	NL80211_RATE_INFO_HE_RU_ALLOC_996                       = 0x5
	NL80211_RATE_INFO_HE_RU_ALLOC                           = 0x11
	NL80211_RATE_INFO_MAX                                   = 0x1d
	NL80211_RATE_INFO_MCS                                   = 0x2
	NL80211_RATE_INFO_S1G_MCS                               = 0x17
	NL80211_RATE_INFO_S1G_NSS                               = 0x18
	NL80211_RATE_INFO_SHORT_GI                              = 0x4
	NL80211_RATE_INFO_VHT_MCS                               = 0x6
	NL80211_RATE_INFO_VHT_NSS                               = 0x7
	NL80211_REGDOM_SET_BY_CORE                              = 0x0
	NL80211_REGDOM_SET_BY_COUNTRY_IE                        = 0x3
	NL80211_REGDOM_SET_BY_DRIVER                            = 0x2
	NL80211_REGDOM_SET_BY_USER                              = 0x1
	NL80211_REGDOM_TYPE_COUNTRY                             = 0x0
	NL80211_REGDOM_TYPE_CUSTOM_WORLD                        = 0x2
	NL80211_REGDOM_TYPE_INTERSECTION                        = 0x3
	NL80211_REGDOM_TYPE_WORLD                               = 0x1
	NL80211_REG_RULE_ATTR_MAX                               = 0x8
	NL80211_REKEY_DATA_AKM                                  = 0x4
	NL80211_REKEY_DATA_KCK                                  = 0x2
	NL80211_REKEY_DATA_KEK                                  = 0x1
	NL80211_REKEY_DATA_REPLAY_CTR                           = 0x3
	NL80211_REPLAY_CTR_LEN                                  = 0x8
	NL80211_RRF_ALLOW_6GHZ_VLP_AP                           = 0x1000000
	NL80211_RRF_AUTO_BW                                     = 0x800
	NL80211_RRF_DFS                                         = 0x10
	NL80211_RRF_DFS_CONCURRENT                              = 0x200000
	NL80211_RRF_GO_CONCURRENT                               = 0x1000
	NL80211_RRF_IR_CONCURRENT                               = 0x1000
	NL80211_RRF_NO_160MHZ                                   = 0x10000
	NL80211_RRF_NO_320MHZ                                   = 0x40000
	NL80211_RRF_NO_6GHZ_AFC_CLIENT                          = 0x800000
	NL80211_RRF_NO_6GHZ_VLP_CLIENT                          = 0x400000
	NL80211_RRF_NO_80MHZ                                    = 0x8000
	NL80211_RRF_NO_CCK                                      = 0x2
	NL80211_RRF_NO_EHT                                      = 0x80000
	NL80211_RRF_NO_HE                                       = 0x20000
	NL80211_RRF_NO_HT40                                     = 0x6000
	NL80211_RRF_NO_HT40MINUS                                = 0x2000
	NL80211_RRF_NO_HT40PLUS                                 = 0x4000
	NL80211_RRF_NO_IBSS                                     = 0x80
	NL80211_RRF_NO_INDOOR                                   = 0x4
	NL80211_RRF_NO_IR_ALL                                   = 0x180
	NL80211_RRF_NO_IR                                       = 0x80
	NL80211_RRF_NO_OFDM                                     = 0x1
	NL80211_RRF_NO_OUTDOOR                                  = 0x8
	NL80211_RRF_NO_UHB_AFC_CLIENT                           = 0x800000
	NL80211_RRF_NO_UHB_VLP_CLIENT                           = 0x400000
	NL80211_RRF_PASSIVE_SCAN                                = 0x80
	NL80211_RRF_PSD                                         = 0x100000
	NL80211_RRF_PTMP_ONLY                                   = 0x40
	NL80211_RRF_PTP_ONLY                                    = 0x20
	NL80211_RXMGMT_FLAG_ANSWERED                            = 0x1
	NL80211_RXMGMT_FLAG_EXTERNAL_AUTH                       = 0x2
	NL80211_SAE_PWE_BOTH                                    = 0x3
	NL80211_SAE_PWE_HASH_TO_ELEMENT                         = 0x2
	NL80211_SAE_PWE_HUNT_AND_PECK                           = 0x1
	NL80211_SAE_PWE_UNSPECIFIED                             = 0x0
	NL80211_SAR_ATTR_MAX                                    = 0x2
	NL80211_SAR_ATTR_SPECS                                  = 0x2
	NL80211_SAR_ATTR_SPECS_END_FREQ                         = 0x4
	NL80211_SAR_ATTR_SPECS_MAX                              = 0x4
	NL80211_SAR_ATTR_SPECS_POWER                            = 0x1
	NL80211_SAR_ATTR_SPECS_RANGE_INDEX                      = 0x2
	NL80211_SAR_ATTR_SPECS_START_FREQ                       = 0x3
	NL80211_SAR_ATTR_TYPE                                   = 0x1
	NL80211_SAR_TYPE_POWER                                  = 0x0
	NL80211_SCAN_FLAG_ACCEPT_BCAST_PROBE_RESP               = 0x20
	NL80211_SCAN_FLAG_AP                                    = 0x4
	NL80211_SCAN_FLAG_COLOCATED_6GHZ                        = 0x4000
	NL80211_SCAN_FLAG_FILS_MAX_CHANNEL_TIME                 = 0x10
	NL80211_SCAN_FLAG_FLUSH                                 = 0x2
	NL80211_SCAN_FLAG_FREQ_KHZ                              = 0x2000
	NL80211_SCAN_FLAG_HIGH_ACCURACY                         = 0x400
	NL80211_SCAN_FLAG_LOW_POWER                             = 0x200
	NL80211_SCAN_FLAG_LOW_PRIORITY                          = 0x1
	NL80211_SCAN_FLAG_LOW_SPAN                              = 0x100
	NL80211_SCAN_FLAG_MIN_PREQ_CONTENT                      = 0x1000
	NL80211_SCAN_FLAG_OCE_PROBE_REQ_DEFERRAL_SUPPRESSION    = 0x80
	NL80211_SCAN_FLAG_OCE_PROBE_REQ_HIGH_TX_RATE            = 0x40
	NL80211_SCAN_FLAG_RANDOM_ADDR                           = 0x8
	NL80211_SCAN_FLAG_RANDOM_SN                             = 0x800
	NL80211_SCAN_RSSI_THOLD_OFF                             = -0x12c
	NL80211_SCHED_SCAN_MATCH_ATTR_BSSID                     = 0x5
	NL80211_SCHED_SCAN_MATCH_ATTR_MAX                       = 0x6
	NL80211_SCHED_SCAN_MATCH_ATTR_RELATIVE_RSSI             = 0x3
	NL80211_SCHED_SCAN_MATCH_ATTR_RSSI_ADJUST               = 0x4
	NL80211_SCHED_SCAN_MATCH_ATTR_RSSI                      = 0x2
	NL80211_SCHED_SCAN_MATCH_ATTR_SSID                      = 0x1
	NL80211_SCHED_SCAN_MATCH_PER_BAND_RSSI                  = 0x6
	NL80211_SCHED_SCAN_PLAN_INTERVAL                        = 0x1
	NL80211_SCHED_SCAN_PLAN_ITERATIONS                      = 0x2
	NL80211_SCHED_SCAN_PLAN_MAX                             = 0x2
	NL80211_SMPS_DYNAMIC                                    = 0x2
	NL80211_SMPS_MAX                                        = 0x2
	NL80211_SMPS_OFF                                        = 0x0
	NL80211_SMPS_STATIC                                     = 0x1
	NL80211_STA_BSS_PARAM_BEACON_INTERVAL                   = 0x5
	NL80211_STA_BSS_PARAM_CTS_PROT                          = 0x1
	NL80211_STA_BSS_PARAM_DTIM_PERIOD                       = 0x4
	NL80211_STA_BSS_PARAM_MAX                               = 0x5
	NL80211_STA_BSS_PARAM_SHORT_PREAMBLE                    = 0x2
	NL80211_STA_BSS_PARAM_SHORT_SLOT_TIME                   = 0x3
	NL80211_STA_FLAG_ASSOCIATED                             = 0x7
	NL80211_STA_FLAG_AUTHENTICATED                          = 0x5
	NL80211_STA_FLAG_AUTHORIZED                             = 0x1
	NL80211_STA_FLAG_MAX                                    = 0x8
	NL80211_STA_FLAG_MAX_OLD_API                            = 0x6
	NL80211_STA_FLAG_MFP                                    = 0x4
	NL80211_STA_FLAG_SHORT_PREAMBLE                         = 0x2
	NL80211_STA_FLAG_SPP_AMSDU                              = 0x8
	NL80211_STA_FLAG_TDLS_PEER                              = 0x6
	NL80211_STA_FLAG_WME                                    = 0x3
	NL80211_STA_INFO_ACK_SIGNAL_AVG                         = 0x23
	NL80211_STA_INFO_ACK_SIGNAL                             = 0x22
	NL80211_STA_INFO_AIRTIME_LINK_METRIC                    = 0x29
	NL80211_STA_INFO_AIRTIME_WEIGHT                         = 0x28
	NL80211_STA_INFO_ASSOC_AT_BOOTTIME                      = 0x2a
	NL80211_STA_INFO_BEACON_LOSS                            = 0x12
	NL80211_STA_INFO_BEACON_RX                              = 0x1d
	NL80211_STA_INFO_BEACON_SIGNAL_AVG                      = 0x1e
	NL80211_STA_INFO_BSS_PARAM                              = 0xf
	NL80211_STA_INFO_CHAIN_SIGNAL_AVG                       = 0x1a
	NL80211_STA_INFO_CHAIN_SIGNAL                           = 0x19
	NL80211_STA_INFO_CONNECTED_TIME                         = 0x10
	NL80211_STA_INFO_CONNECTED_TO_AS                        = 0x2b
	NL80211_STA_INFO_CONNECTED_TO_GATE                      = 0x26
	NL80211_STA_INFO_DATA_ACK_SIGNAL_AVG                    = 0x23
	NL80211_STA_INFO_EXPECTED_THROUGHPUT                    = 0x1b
	NL80211_STA_INFO_FCS_ERROR_COUNT                        = 0x25
	NL80211_STA_INFO_INACTIVE_TIME                          = 0x1
	NL80211_STA_INFO_LLID                                   = 0x4
	NL80211_STA_INFO_LOCAL_PM                               = 0x14
	NL80211_STA_INFO_MAX                                    = 0x2b
	NL80211_STA_INFO_NONPEER_PM                             = 0x16
	NL80211_STA_INFO_PAD                                    = 0x21
	NL80211_STA_INFO_PEER_PM                                = 0x15
	NL80211_STA_INFO_PLID                                   = 0x5
	NL80211_STA_INFO_PLINK_STATE                            = 0x6
	NL80211_STA_INFO_RX_BITRATE                             = 0xe
	NL80211_STA_INFO_RX_BYTES64                             = 0x17
	NL80211_STA_INFO_RX_BYTES                               = 0x2
	NL80211_STA_INFO_RX_DROP_MISC                           = 0x1c
	NL80211_STA_INFO_RX_DURATION                            = 0x20
	NL80211_STA_INFO_RX_MPDUS                               = 0x24
	NL80211_STA_INFO_RX_PACKETS                             = 0x9
	NL80211_STA_INFO_SIGNAL_AVG                             = 0xd
	NL80211_STA_INFO_SIGNAL                                 = 0x7
	NL80211_STA_INFO_STA_FLAGS                              = 0x11
	NL80211_STA_INFO_TID_STATS                              = 0x1f
	NL80211_STA_INFO_T_OFFSET                               = 0x13
	NL80211_STA_INFO_TX_BITRATE                             = 0x8
	NL80211_STA_INFO_TX_BYTES64                             = 0x18
	NL80211_STA_INFO_TX_BYTES                               = 0x3
	NL80211_STA_INFO_TX_DURATION                            = 0x27
	NL80211_STA_INFO_TX_FAILED                              = 0xc
	NL80211_STA_INFO_TX_PACKETS                             = 0xa
	NL80211_STA_INFO_TX_RETRIES                             = 0xb
	NL80211_STA_WME_MAX                                     = 0x2
	NL80211_STA_WME_MAX_SP                                  = 0x2
	NL80211_STA_WME_UAPSD_QUEUES                            = 0x1
	NL80211_SURVEY_INFO_CHANNEL_TIME_BUSY                   = 0x5
	NL80211_SURVEY_INFO_CHANNEL_TIME                        = 0x4
	NL80211_SURVEY_INFO_CHANNEL_TIME_EXT_BUSY               = 0x6
	NL80211_SURVEY_INFO_CHANNEL_TIME_RX                     = 0x7
	NL80211_SURVEY_INFO_CHANNEL_TIME_TX                     = 0x8
	NL80211_SURVEY_INFO_FREQUENCY                           = 0x1
	NL80211_SURVEY_INFO_FREQUENCY_OFFSET                    = 0xc
	NL80211_SURVEY_INFO_IN_USE                              = 0x3
	NL80211_SURVEY_INFO_MAX                                 = 0xc
	NL80211_SURVEY_INFO_NOISE                               = 0x2
	NL80211_SURVEY_INFO_PAD                                 = 0xa
	NL80211_SURVEY_INFO_TIME_BSS_RX                         = 0xb
	NL80211_SURVEY_INFO_TIME_BUSY                           = 0x5
	NL80211_SURVEY_INFO_TIME                                = 0x4
	NL80211_SURVEY_INFO_TIME_EXT_BUSY                       = 0x6
	NL80211_SURVEY_INFO_TIME_RX                             = 0x7
	NL80211_SURVEY_INFO_TIME_SCAN                           = 0x9
	NL80211_SURVEY_INFO_TIME_TX                             = 0x8
	NL80211_TDLS_DISABLE_LINK                               = 0x4
	NL80211_TDLS_DISCOVERY_REQ                              = 0x0
	NL80211_TDLS_ENABLE_LINK                                = 0x3
	NL80211_TDLS_PEER_HE                                    = 0x8
	NL80211_TDLS_PEER_HT                                    = 0x1
	NL80211_TDLS_PEER_VHT                                   = 0x2
	NL80211_TDLS_PEER_WMM                                   = 0x4
	NL80211_TDLS_SETUP                                      = 0x1
	NL80211_TDLS_TEARDOWN                                   = 0x2
	NL80211_TID_CONFIG_ATTR_AMPDU_CTRL                      = 0x9
	NL80211_TID_CONFIG_ATTR_AMSDU_CTRL                      = 0xb
	NL80211_TID_CONFIG_ATTR_MAX                             = 0xd
	NL80211_TID_CONFIG_ATTR_NOACK                           = 0x6
	NL80211_TID_CONFIG_ATTR_OVERRIDE                        = 0x4
	NL80211_TID_CONFIG_ATTR_PAD                             = 0x1
	NL80211_TID_CONFIG_ATTR_PEER_SUPP                       = 0x3
	NL80211_TID_CONFIG_ATTR_RETRY_LONG                      = 0x8
	NL80211_TID_CONFIG_ATTR_RETRY_SHORT                     = 0x7
	NL80211_TID_CONFIG_ATTR_RTSCTS_CTRL                     = 0xa
	NL80211_TID_CONFIG_ATTR_TIDS                            = 0x5
	NL80211_TID_CONFIG_ATTR_TX_RATE                         = 0xd
	NL80211_TID_CONFIG_ATTR_TX_RATE_TYPE                    = 0xc
	NL80211_TID_CONFIG_ATTR_VIF_SUPP                        = 0x2
	NL80211_TID_CONFIG_DISABLE                              = 0x1
	NL80211_TID_CONFIG_ENABLE                               = 0x0
	NL80211_TID_STATS_MAX                                   = 0x6
	NL80211_TID_STATS_PAD                                   = 0x5
	NL80211_TID_STATS_RX_MSDU                               = 0x1
	NL80211_TID_STATS_TX_MSDU                               = 0x2
	NL80211_TID_STATS_TX_MSDU_FAILED                        = 0x4
	NL80211_TID_STATS_TX_MSDU_RETRIES                       = 0x3
	NL80211_TID_STATS_TXQ_STATS                             = 0x6
	NL80211_TIMEOUT_ASSOC                                   = 0x3
	NL80211_TIMEOUT_AUTH                                    = 0x2
	NL80211_TIMEOUT_SCAN                                    = 0x1
	NL80211_TIMEOUT_UNSPECIFIED                             = 0x0
	NL80211_TKIP_DATA_OFFSET_ENCR_KEY                       = 0x0
	NL80211_TKIP_DATA_OFFSET_RX_MIC_KEY                     = 0x18
	NL80211_TKIP_DATA_OFFSET_TX_MIC_KEY                     = 0x10
	NL80211_TX_POWER_AUTOMATIC                              = 0x0
	NL80211_TX_POWER_FIXED                                  = 0x2
	NL80211_TX_POWER_LIMITED                                = 0x1
	NL80211_TXQ_ATTR_AC                                     = 0x1
	NL80211_TXQ_ATTR_AIFS                                   = 0x5
	NL80211_TXQ_ATTR_CWMAX                                  = 0x4
	NL80211_TXQ_ATTR_CWMIN                                  = 0x3
	NL80211_TXQ_ATTR_MAX                                    = 0x5
	NL80211_TXQ_ATTR_QUEUE                                  = 0x1
	NL80211_TXQ_ATTR_TXOP                                   = 0x2
	NL80211_TXQ_Q_BE                                        = 0x2
	NL80211_TXQ_Q_BK                                        = 0x3
	NL80211_TXQ_Q_VI                                        = 0x1
	NL80211_TXQ_Q_VO                                        = 0x0
	NL80211_TXQ_STATS_BACKLOG_BYTES                         = 0x1
	NL80211_TXQ_STATS_BACKLOG_PACKETS                       = 0x2
	NL80211_TXQ_STATS_COLLISIONS                            = 0x8
	NL80211_TXQ_STATS_DROPS                                 = 0x4
	NL80211_TXQ_STATS_ECN_MARKS                             = 0x5
	NL80211_TXQ_STATS_FLOWS                                 = 0x3
	NL80211_TXQ_STATS_MAX                                   = 0xb
	NL80211_TXQ_STATS_MAX_FLOWS                             = 0xb
	NL80211_TXQ_STATS_OVERLIMIT                             = 0x6
	NL80211_TXQ_STATS_OVERMEMORY                            = 0x7
	NL80211_TXQ_STATS_TX_BYTES                              = 0x9
	NL80211_TXQ_STATS_TX_PACKETS                            = 0xa
	NL80211_TX_RATE_AUTOMATIC                               = 0x0
	NL80211_TXRATE_DEFAULT_GI                               = 0x0
	NL80211_TX_RATE_FIXED                                   = 0x2
	NL80211_TXRATE_FORCE_LGI                                = 0x2
	NL80211_TXRATE_FORCE_SGI                                = 0x1
	NL80211_TXRATE_GI                                       = 0x4
	NL80211_TXRATE_HE                                       = 0x5
	NL80211_TXRATE_HE_GI                                    = 0x6
	NL80211_TXRATE_HE_LTF                                   = 0x7
	NL80211_TXRATE_HT                                       = 0x2
	NL80211_TXRATE_LEGACY                                   = 0x1
	NL80211_TX_RATE_LIMITED                                 = 0x1
	NL80211_TXRATE_MAX                                      = 0x7
	NL80211_TXRATE_MCS                                      = 0x2
	NL80211_TXRATE_VHT                                      = 0x3
	NL80211_UNSOL_BCAST_PROBE_RESP_ATTR_INT                 = 0x1
	NL80211_UNSOL_BCAST_PROBE_RESP_ATTR_MAX                 = 0x2
	NL80211_UNSOL_BCAST_PROBE_RESP_ATTR_TMPL                = 0x2
	NL80211_USER_REG_HINT_CELL_BASE                         = 0x1
	NL80211_USER_REG_HINT_INDOOR                            = 0x2
	NL80211_USER_REG_HINT_USER                              = 0x0
	NL80211_VENDOR_ID_IS_LINUX                              = 0x80000000
	NL80211_VHT_CAPABILITY_LEN                              = 0xc
	NL80211_VHT_NSS_MAX                                     = 0x8
	NL80211_WIPHY_NAME_MAXLEN                               = 0x40
	NL80211_WIPHY_RADIO_ATTR_FREQ_RANGE                     = 0x2
	NL80211_WIPHY_RADIO_ATTR_INDEX                          = 0x1
	NL80211_WIPHY_RADIO_ATTR_INTERFACE_COMBINATION          = 0x3
	NL80211_WIPHY_RADIO_ATTR_MAX                            = 0x4
	NL80211_WIPHY_RADIO_FREQ_ATTR_END                       = 0x2
	NL80211_WIPHY_RADIO_FREQ_ATTR_MAX                       = 0x2
	NL80211_WIPHY_RADIO_FREQ_ATTR_START                     = 0x1
	NL80211_WMMR_AIFSN                                      = 0x3
	NL80211_WMMR_CW_MAX                                     = 0x2
	NL80211_WMMR_CW_MIN                                     = 0x1
	NL80211_WMMR_MAX                                        = 0x4
	NL80211_WMMR_TXOP                                       = 0x4
	NL80211_WOWLAN_PKTPAT_MASK                              = 0x1
	NL80211_WOWLAN_PKTPAT_OFFSET                            = 0x3
	NL80211_WOWLAN_PKTPAT_PATTERN                           = 0x2
	NL80211_WOWLAN_TCP_DATA_INTERVAL                        = 0x9
	NL80211_WOWLAN_TCP_DATA_PAYLOAD                         = 0x6
	NL80211_WOWLAN_TCP_DATA_PAYLOAD_SEQ                     = 0x7
	NL80211_WOWLAN_TCP_DATA_PAYLOAD_TOKEN                   = 0x8
	NL80211_WOWLAN_TCP_DST_IPV4                             = 0x2
	NL80211_WOWLAN_TCP_DST_MAC                              = 0x3
	NL80211_WOWLAN_TCP_DST_PORT                             = 0x5
	NL80211_WOWLAN_TCP_SRC_IPV4                             = 0x1
	NL80211_WOWLAN_TCP_SRC_PORT                             = 0x4
	NL80211_WOWLAN_TCP_WAKE_MASK                            = 0xb
	NL80211_WOWLAN_TCP_WAKE_PAYLOAD                         = 0xa
	NL80211_WOWLAN_TRIG_4WAY_HANDSHAKE                      = 0x8
	NL80211_WOWLAN_TRIG_ANY                                 = 0x1
	NL80211_WOWLAN_TRIG_DISCONNECT                          = 0x2
	NL80211_WOWLAN_TRIG_EAP_IDENT_REQUEST                   = 0x7
	NL80211_WOWLAN_TRIG_GTK_REKEY_FAILURE                   = 0x6
	NL80211_WOWLAN_TRIG_GTK_REKEY_SUPPORTED                 = 0x5
	NL80211_WOWLAN_TRIG_MAGIC_PKT                           = 0x3
	NL80211_WOWLAN_TRIG_NET_DETECT                          = 0x12
	NL80211_WOWLAN_TRIG_NET_DETECT_RESULTS                  = 0x13
	NL80211_WOWLAN_TRIG_PKT_PATTERN                         = 0x4
	NL80211_WOWLAN_TRIG_RFKILL_RELEASE                      = 0x9
	NL80211_WOWLAN_TRIG_TCP_CONNECTION                      = 0xe
	NL80211_WOWLAN_TRIG_UNPROTECTED_DEAUTH_DISASSOC         = 0x14
	NL80211_WOWLAN_TRIG_WAKEUP_PKT_80211                    = 0xa
	NL80211_WOWLAN_TRIG_WAKEUP_PKT_80211_LEN                = 0xb
	NL80211_WOWLAN_TRIG_WAKEUP_PKT_8023                     = 0xc
	NL80211_WOWLAN_TRIG_WAKEUP_PKT_8023_LEN                 = 0xd
	NL80211_WOWLAN_TRIG_WAKEUP_TCP_CONNLOST                 = 0x10
	NL80211_WOWLAN_TRIG_WAKEUP_TCP_MATCH                    = 0xf
	NL80211_WOWLAN_TRIG_WAKEUP_TCP_NOMORETOKENS             = 0x11
	NL80211_WPA_VERSION_1                                   = 0x1
	NL80211_WPA_VERSION_2                                   = 0x2
	NL80211_WPA_VERSION_3                                   = 0x4
)

const (
	FRA_UNSPEC             = 0x0
	FRA_DST                = 0x1
	FRA_SRC                = 0x2
	FRA_IIFNAME            = 0x3
	FRA_GOTO               = 0x4
	FRA_UNUSED2            = 0x5
	FRA_PRIORITY           = 0x6
	FRA_UNUSED3            = 0x7
	FRA_UNUSED4            = 0x8
	FRA_UNUSED5            = 0x9
	FRA_FWMARK             = 0xa
	FRA_FLOW               = 0xb
	FRA_TUN_ID             = 0xc
	FRA_SUPPRESS_IFGROUP   = 0xd
	FRA_SUPPRESS_PREFIXLEN = 0xe
	FRA_TABLE              = 0xf
	FRA_FWMASK             = 0x10
	FRA_OIFNAME            = 0x11
	FRA_PAD                = 0x12
	FRA_L3MDEV             = 0x13
	FRA_UID_RANGE          = 0x14
	FRA_PROTOCOL           = 0x15
	FRA_IP_PROTO           = 0x16
	FRA_SPORT_RANGE        = 0x17
	FRA_DPORT_RANGE        = 0x18
	FR_ACT_UNSPEC          = 0x0
	FR_ACT_TO_TBL          = 0x1
	FR_ACT_GOTO            = 0x2
	FR_ACT_NOP             = 0x3
	FR_ACT_RES3            = 0x4
	FR_ACT_RES4            = 0x5
	FR_ACT_BLACKHOLE       = 0x6
	FR_ACT_UNREACHABLE     = 0x7
	FR_ACT_PROHIBIT        = 0x8
)

const (
	AUDIT_NLGRP_NONE    = 0x0
	AUDIT_NLGRP_READLOG = 0x1
)

const (
	TUN_F_CSUM    = 0x1
	TUN_F_TSO4    = 0x2
	TUN_F_TSO6    = 0x4
	TUN_F_TSO_ECN = 0x8
	TUN_F_UFO     = 0x10
	TUN_F_USO4    = 0x20
	TUN_F_USO6    = 0x40
)

const (
	VIRTIO_NET_HDR_F_NEEDS_CSUM = 0x1
	VIRTIO_NET_HDR_F_DATA_VALID = 0x2
	VIRTIO_NET_HDR_F_RSC_INFO   = 0x4
)

const (
	VIRTIO_NET_HDR_GSO_NONE   = 0x0
	VIRTIO_NET_HDR_GSO_TCPV4  = 0x1
	VIRTIO_NET_HDR_GSO_UDP    = 0x3
	VIRTIO_NET_HDR_GSO_TCPV6  = 0x4
	VIRTIO_NET_HDR_GSO_UDP_L4 = 0x5
	VIRTIO_NET_HDR_GSO_ECN    = 0x80
)

type SchedAttr struct {
	Size     uint32
	Policy   uint32
	Flags    uint64
	Nice     int32
	Priority uint32
	Runtime  uint64
	Deadline uint64
	Period   uint64
	Util_min uint32
	Util_max uint32
}

const SizeofSchedAttr = 0x38

type Cachestat_t struct {
	Cache            uint64
	Dirty            uint64
	Writeback        uint64
	Evicted          uint64
	Recently_evicted uint64
}
type CachestatRange struct {
	Off uint64
	Len uint64
}

const (
	SK_MEMINFO_RMEM_ALLOC          = 0x0
	SK_MEMINFO_RCVBUF              = 0x1
	SK_MEMINFO_WMEM_ALLOC          = 0x2
	SK_MEMINFO_SNDBUF              = 0x3
	SK_MEMINFO_FWD_ALLOC           = 0x4
	SK_MEMINFO_WMEM_QUEUED         = 0x5
	SK_MEMINFO_OPTMEM              = 0x6
	SK_MEMINFO_BACKLOG             = 0x7
	SK_MEMINFO_DROPS               = 0x8
	SK_MEMINFO_VARS                = 0x9
	SKNLGRP_NONE                   = 0x0
	SKNLGRP_INET_TCP_DESTROY       = 0x1
	SKNLGRP_INET_UDP_DESTROY       = 0x2
	SKNLGRP_INET6_TCP_DESTROY      = 0x3
	SKNLGRP_INET6_UDP_DESTROY      = 0x4
	SK_DIAG_BPF_STORAGE_REQ_NONE   = 0x0
	SK_DIAG_BPF_STORAGE_REQ_MAP_FD = 0x1
	SK_DIAG_BPF_STORAGE_REP_NONE   = 0x0
	SK_DIAG_BPF_STORAGE            = 0x1
	SK_DIAG_BPF_STORAGE_NONE       = 0x0
	SK_DIAG_BPF_STORAGE_PAD        = 0x1
	SK_DIAG_BPF_STORAGE_MAP_ID     = 0x2
	SK_DIAG_BPF_STORAGE_MAP_VALUE  = 0x3
)

type SockDiagReq struct {
	Family   uint8
	Protocol uint8
}

const RTM_NEWNVLAN = 0x70

const (
	SizeofPtr  = 0x8
	SizeofLong = 0x8
)

type (
	_C_long int64
)

type Timespec struct {
	Sec  int64
	Nsec int64
}

type Timeval struct {
	Sec  int64
	Usec int64
}

type Timex struct {
	Modes     uint32
	Offset    int64
	Freq      int64
	Maxerror  int64
	Esterror  int64
	Status    int32
	Constant  int64
	Precision int64
	Tolerance int64
	Time      Timeval
	Tick      int64
	Ppsfreq   int64
	Jitter    int64
	Shift     int32
	Stabil    int64
	Jitcnt    int64
	Calcnt    int64
	Errcnt    int64
	Stbcnt    int64
	Tai       int32
	_         [44]byte
}

type Time_t int64

type Tms struct {
	Utime  int64
	Stime  int64
	Cutime int64
	Cstime int64
}

type Utimbuf struct {
	Actime  int64
	Modtime int64
}

type Rusage struct {
	Utime    Timeval
	Stime    Timeval
	Maxrss   int64
	Ixrss    int64
	Idrss    int64
	Isrss    int64
	Minflt   int64
	Majflt   int64
	Nswap    int64
	Inblock  int64
	Oublock  int64
	Msgsnd   int64
	Msgrcv   int64
	Nsignals int64
	Nvcsw    int64
	Nivcsw   int64
}

type Stat_t struct {
	Dev     uint64
	Ino     uint64
	Nlink   uint64
	Mode    uint32
	Uid     uint32
	Gid     uint32
	_       int32
	Rdev    uint64
	Size    int64
	Blksize int64
	Blocks  int64
	Atim    Timespec
	Mtim    Timespec
	Ctim    Timespec
	_       [3]int64
}

type Dirent struct {
	Ino    uint64
	Off    int64
	Reclen uint16
	Type   uint8
	Name   [256]int8
	_      [5]byte
}

type Flock_t struct {
	Type   int16
	Whence int16
	Start  int64
	Len    int64
	Pid    int32
	_      [4]byte
}

type DmNameList struct {
	Dev  uint64
	Next uint32
	Name [0]byte
	_    [4]byte
}

const (
	FADV_DONTNEED = 0x4
	FADV_NOREUSE  = 0x5
)

type RawSockaddrNFCLLCP struct {
	Sa_family        uint16
	Dev_idx          uint32
	Target_idx       uint32
	Nfc_protocol     uint32
	Dsap             uint8
	Ssap             uint8
	Service_name     [63]uint8
	Service_name_len uint64
}

type RawSockaddr struct {
	Family uint16
	Data   [14]int8
}

type RawSockaddrAny struct {
	Addr RawSockaddr
	Pad  [96]int8
}

type Iovec struct {
	Base *byte
	Len  uint64
}

type Msghdr struct {
	Name       *byte
	Namelen    uint32
	Iov        *Iovec
	Iovlen     uint64
	Control    *byte
	Controllen uint64
	Flags      int32
	_          [4]byte
}

type Cmsghdr struct {
	Len   uint64
	Level int32
	Type  int32
}

type ifreq struct {
	Ifrn [16]byte
	Ifru [24]byte
}

const (
	SizeofSockaddrNFCLLCP = 0x60
	SizeofIovec           = 0x10
	SizeofMsghdr          = 0x38
	SizeofCmsghdr         = 0x10
)

const (
	SizeofSockFprog = 0x10
)

type PtraceRegs struct {
	R15      uint64
	R14      uint64
	R13      uint64
	R12      uint64
	Rbp      uint64
	Rbx      uint64
	R11      uint64
	R10      uint64
	R9       uint64
	R8       uint64
	Rax      uint64
	Rcx      uint64
	Rdx      uint64
	Rsi      uint64
	Rdi      uint64
	Orig_rax uint64
	Rip      uint64
	Cs       uint64
	Eflags   uint64
	Rsp      uint64
	Ss       uint64
	Fs_base  uint64
	Gs_base  uint64
	Ds       uint64
	Es       uint64
	Fs       uint64
	Gs       uint64
}

type FdSet struct {
	Bits [16]int64
}

type Sysinfo_t struct {
	Uptime    int64
	Loads     [3]uint64
	Totalram  uint64
	Freeram   uint64
	Sharedram uint64
	Bufferram uint64
	Totalswap uint64
	Freeswap  uint64
	Procs     uint16
	Pad       uint16
	Totalhigh uint64
	Freehigh  uint64
	Unit      uint32
	_         [0]int8
	_         [4]byte
}

type Ustat_t struct {
	Tfree  int32
	Tinode uint64
	Fname  [6]int8
	Fpack  [6]int8
	_      [4]byte
}

type EpollEvent struct {
	Events uint32
	Fd     int32
	Pad    int32
}

const (
	OPEN_TREE_CLOEXEC = 0x80000
)

const (
	POLLRDHUP = 0x2000
)

type Sigset_t struct {
	Val [16]uint64
}

const _C__NSIG = 0x41

const (
	SIG_BLOCK   = 0x0
	SIG_UNBLOCK = 0x1
	SIG_SETMASK = 0x2
)

type Siginfo struct {
	Signo int32
	Errno int32
	Code  int32
	_     int32
	_     [112]byte
}

type Termios struct {
	Iflag  uint32
	Oflag  uint32
	Cflag  uint32
	Lflag  uint32
	Line   uint8
	Cc     [19]uint8
	Ispeed uint32
	Ospeed uint32
}

type Taskstats struct {
	Version                   uint16
	Ac_exitcode               uint32
	Ac_flag                   uint8
	Ac_nice                   uint8
	Cpu_count                 uint64
	Cpu_delay_total           uint64
	Blkio_count               uint64
	Blkio_delay_total         uint64
	Swapin_count              uint64
	Swapin_delay_total        uint64
	Cpu_run_real_total        uint64
	Cpu_run_virtual_total     uint64
	Ac_comm                   [32]int8
	Ac_sched                  uint8
	Ac_pad                    [3]uint8
	_                         [4]byte
	Ac_uid                    uint32
	Ac_gid                    uint32
	Ac_pid                    uint32
	Ac_ppid                   uint32
	Ac_btime                  uint32
	Ac_etime                  uint64
	Ac_utime                  uint64
	Ac_stime                  uint64
	Ac_minflt                 uint64
	Ac_majflt                 uint64
	Coremem                   uint64
	Virtmem                   uint64
	Hiwater_rss               uint64
	Hiwater_vm                uint64
	Read_char                 uint64
	Write_char                uint64
	Read_syscalls             uint64
	Write_syscalls            uint64
	Read_bytes                uint64
	Write_bytes               uint64
	Cancelled_write_bytes     uint64
	Nvcsw                     uint64
	Nivcsw                    uint64
	Ac_utimescaled            uint64
	Ac_stimescaled            uint64
	Cpu_scaled_run_real_total uint64
	Freepages_count           uint64
	Freepages_delay_total     uint64
	Thrashing_count           uint64
	Thrashing_delay_total     uint64
	Ac_btime64                uint64
	Compact_count             uint64
	Compact_delay_total       uint64
	Ac_tgid                   uint32
	Ac_tgetime                uint64
	Ac_exe_dev                uint64
	Ac_exe_inode              uint64
	Wpcopy_count              uint64
	Wpcopy_delay_total        uint64
	Irq_count                 uint64
	Irq_delay_total           uint64
	Cpu_delay_max             uint64
	Cpu_delay_min             uint64
	Blkio_delay_max           uint64
	Blkio_delay_min           uint64
	Swapin_delay_max          uint64
	Swapin_delay_min          uint64
	Freepages_delay_max       uint64
	Freepages_delay_min       uint64
	Thrashing_delay_max       uint64
	Thrashing_delay_min       uint64
	Compact_delay_max         uint64
	Compact_delay_min         uint64
	Wpcopy_delay_max          uint64
	Wpcopy_delay_min          uint64
	Irq_delay_max             uint64
	Irq_delay_min             uint64
}

type cpuMask uint64

const (
	_NCPUBITS = 0x40
)

const (
	CBitFieldMaskBit0  = 0x1
	CBitFieldMaskBit1  = 0x2
	CBitFieldMaskBit2  = 0x4
	CBitFieldMaskBit3  = 0x8
	CBitFieldMaskBit4  = 0x10
	CBitFieldMaskBit5  = 0x20
	CBitFieldMaskBit6  = 0x40
	CBitFieldMaskBit7  = 0x80
	CBitFieldMaskBit8  = 0x100
	CBitFieldMaskBit9  = 0x200
	CBitFieldMaskBit10 = 0x400
	CBitFieldMaskBit11 = 0x800
	CBitFieldMaskBit12 = 0x1000
	CBitFieldMaskBit13 = 0x2000
	CBitFieldMaskBit14 = 0x4000
	CBitFieldMaskBit15 = 0x8000
	CBitFieldMaskBit16 = 0x10000
	CBitFieldMaskBit17 = 0x20000
	CBitFieldMaskBit18 = 0x40000
	CBitFieldMaskBit19 = 0x80000
	CBitFieldMaskBit20 = 0x100000
	CBitFieldMaskBit21 = 0x200000
	CBitFieldMaskBit22 = 0x400000
	CBitFieldMaskBit23 = 0x800000
	CBitFieldMaskBit24 = 0x1000000
	CBitFieldMaskBit25 = 0x2000000
	CBitFieldMaskBit26 = 0x4000000
	CBitFieldMaskBit27 = 0x8000000
	CBitFieldMaskBit28 = 0x10000000
	CBitFieldMaskBit29 = 0x20000000
	CBitFieldMaskBit30 = 0x40000000
	CBitFieldMaskBit31 = 0x80000000
	CBitFieldMaskBit32 = 0x100000000
	CBitFieldMaskBit33 = 0x200000000
	CBitFieldMaskBit34 = 0x400000000
	CBitFieldMaskBit35 = 0x800000000
	CBitFieldMaskBit36 = 0x1000000000
	CBitFieldMaskBit37 = 0x2000000000
	CBitFieldMaskBit38 = 0x4000000000
	CBitFieldMaskBit39 = 0x8000000000
	CBitFieldMaskBit40 = 0x10000000000
	CBitFieldMaskBit41 = 0x20000000000
	CBitFieldMaskBit42 = 0x40000000000
	CBitFieldMaskBit43 = 0x80000000000
	CBitFieldMaskBit44 = 0x100000000000
	CBitFieldMaskBit45 = 0x200000000000
	CBitFieldMaskBit46 = 0x400000000000
	CBitFieldMaskBit47 = 0x800000000000
	CBitFieldMaskBit48 = 0x1000000000000
	CBitFieldMaskBit49 = 0x2000000000000
	CBitFieldMaskBit50 = 0x4000000000000
	CBitFieldMaskBit51 = 0x8000000000000
	CBitFieldMaskBit52 = 0x10000000000000
	CBitFieldMaskBit53 = 0x20000000000000
	CBitFieldMaskBit54 = 0x40000000000000
	CBitFieldMaskBit55 = 0x80000000000000
	CBitFieldMaskBit56 = 0x100000000000000
	CBitFieldMaskBit57 = 0x200000000000000
	CBitFieldMaskBit58 = 0x400000000000000
	CBitFieldMaskBit59 = 0x800000000000000
	CBitFieldMaskBit60 = 0x1000000000000000
	CBitFieldMaskBit61 = 0x2000000000000000
	CBitFieldMaskBit62 = 0x4000000000000000
	CBitFieldMaskBit63 = 0x8000000000000000
)

type SockaddrStorage struct {
	Family uint16
	Data   [118]byte
	_      uint64
}

type HDGeometry struct {
	Heads     uint8
	Sectors   uint8
	Cylinders uint16
	Start     uint64
}

type Statfs_t struct {
	Type    int64
	Bsize   int64
	Blocks  uint64
	Bfree   uint64
	Bavail  uint64
	Files   uint64
	Ffree   uint64
	Fsid    Fsid
	Namelen int64
	Frsize  int64
	Flags   int64
	Spare   [4]int64
}

type TpacketHdr struct {
	Status  uint64
	Len     uint32
	Snaplen uint32
	Mac     uint16
	Net     uint16
	Sec     uint32
	Usec    uint32
	_       [4]byte
}

const (
	SizeofTpacketHdr = 0x20
)

type RTCPLLInfo struct {
	Ctrl    int32
	Value   int32
	Max     int32
	Min     int32
	Posmult int32
	Negmult int32
	Clock   int64
}

type BlkpgPartition struct {
	Start   int64
	Length  int64
	Pno     int32
	Devname [64]uint8
	Volname [64]uint8
	_       [4]byte
}

const (
	BLKPG = 0x1269
)

type CryptoUserAlg struct {
	Name        [64]int8
	Driver_name [64]int8
	Module_name [64]int8
	Type        uint32
	Mask        uint32
	Refcnt      uint32
	Flags       uint32
}

type CryptoStatAEAD struct {
	Type         [64]int8
	Encrypt_cnt  uint64
	Encrypt_tlen uint64
	Decrypt_cnt  uint64
	Decrypt_tlen uint64
	Err_cnt      uint64
}

type CryptoStatAKCipher struct {
	Type         [64]int8
	Encrypt_cnt  uint64
	Encrypt_tlen uint64
	Decrypt_cnt  uint64
	Decrypt_tlen uint64
	Verify_cnt   uint64
	Sign_cnt     uint64
	Err_cnt      uint64
}

type CryptoStatCipher struct {
	Type         [64]int8
	Encrypt_cnt  uint64
	Encrypt_tlen uint64
	Decrypt_cnt  uint64
	Decrypt_tlen uint64
	Err_cnt      uint64
}

type CryptoStatCompress struct {
	Type            [64]int8
	Compress_cnt    uint64
	Compress_tlen   uint64
	Decompress_cnt  uint64
	Decompress_tlen uint64
	Err_cnt         uint64
}

type CryptoStatHash struct {
	Type      [64]int8
	Hash_cnt  uint64
	Hash_tlen uint64
	Err_cnt   uint64
}

type CryptoStatKPP struct {
	Type                      [64]int8
	Setsecret_cnt             uint64
	Generate_public_key_cnt   uint64
	Compute_shared_secret_cnt uint64
	Err_cnt                   uint64
}

type CryptoStatRNG struct {
	Type          [64]int8
	Generate_cnt  uint64
	Generate_tlen uint64
	Seed_cnt      uint64
	Err_cnt       uint64
}

type CryptoStatLarval struct {
	Type [64]int8
}

type CryptoReportLarval struct {
	Type [64]int8
}

type CryptoReportHash struct {
	Type       [64]int8
	Blocksize  uint32
	Digestsize uint32
}

type CryptoReportCipher struct {
	Type        [64]int8
	Blocksize   uint32
	Min_keysize uint32
	Max_keysize uint32
}

type CryptoReportBlkCipher struct {
	Type        [64]int8
	Geniv       [64]int8
	Blocksize   uint32
	Min_keysize uint32
	Max_keysize uint32
	Ivsize      uint32
}

type CryptoReportAEAD struct {
	Type        [64]int8
	Geniv       [64]int8
	Blocksize   uint32
	Maxauthsize uint32
	Ivsize      uint32
}

type CryptoReportComp struct {
	Type [64]int8
}

type CryptoReportRNG struct {
	Type     [64]int8
	Seedsize uint32
}

type CryptoReportAKCipher struct {
	Type [64]int8
}

type CryptoReportKPP struct {
	Type [64]int8
}

type CryptoReportAcomp struct {
	Type [64]int8
}

type LoopInfo struct {
	Number           int32
	Device           uint64
	Inode            uint64
	Rdevice          uint64
	Offset           int32
	Encrypt_type     int32
	Encrypt_key_size int32
	Flags            int32
	Name             [64]int8
	Encrypt_key      [32]uint8
	Init             [2]uint64
	Reserved         [4]int8
	_                [4]byte
}

type TIPCSubscr struct {
	Seq     TIPCServiceRange
	Timeout uint32
	Filter  uint32
	Handle  [8]int8
}

type TIPCSIOCLNReq struct {
	Peer     uint32
	Id       uint32
	Linkname [68]int8
}

type TIPCSIOCNodeIDReq struct {
	Peer uint32
	Id   [16]int8
}

type PPSKInfo struct {
	Assert_sequence uint32
	Clear_sequence  uint32
	Assert_tu       PPSKTime
	Clear_tu        PPSKTime
	Current_mode    int32
	_               [4]byte
}

const (
	PPS_GETPARAMS = 0x800870a1
	PPS_SETPARAMS = 0x400870a2
	PPS_GETCAP    = 0x800870a3
	PPS_FETCH     = 0xc00870a4
)

const (
	PIDFD_NONBLOCK = 0x800
)

type SysvIpcPerm struct {
	Key  int32
	Uid  uint32
	Gid  uint32
	Cuid uint32
	Cgid uint32
	Mode uint32
	_    [0]uint8
	Seq  uint16
	_    uint16
	_    uint64
	_    uint64
}
type SysvShmDesc struct {
	Perm   SysvIpcPerm
	Segsz  uint64
	Atime  int64
	Dtime  int64
	Ctime  int64
	Cpid   int32
	Lpid   int32
	Nattch uint64
	_      uint64
	_      uint64
}

// Mkdev returns a Linux device number generated from the given major and minor
// components.
func Mkdev(major, minor uint32) uint64 {
	dev := (uint64(major) & 0x00000fff) << 8
	dev |= (uint64(major) & 0xfffff000) << 32
	dev |= (uint64(minor) & 0x000000ff) << 0
	dev |= (uint64(minor) & 0xffffff00) << 12
	return dev
}

const (
	B1000000                         = 0x1008
	B115200                          = 0x1002
	B1152000                         = 0x1009
	B1500000                         = 0x100a
	B2000000                         = 0x100b
	B230400                          = 0x1003
	B2500000                         = 0x100c
	B3000000                         = 0x100d
	B3500000                         = 0x100e
	B4000000                         = 0x100f
	B460800                          = 0x1004
	B500000                          = 0x1005
	B57600                           = 0x1001
	B576000                          = 0x1006
	B921600                          = 0x1007
	BLKALIGNOFF                      = 0x127a
	BLKBSZGET                        = 0x80081270
	BLKBSZSET                        = 0x40081271
	BLKDISCARD                       = 0x1277
	BLKDISCARDZEROES                 = 0x127c
	BLKFLSBUF                        = 0x1261
	BLKFRAGET                        = 0x1265
	BLKFRASET                        = 0x1264
	BLKGETDISKSEQ                    = 0x80081280
	BLKGETSIZE                       = 0x1260
	BLKGETSIZE64                     = 0x80081272
	BLKIOMIN                         = 0x1278
	BLKIOOPT                         = 0x1279
	BLKPBSZGET                       = 0x127b
	BLKRAGET                         = 0x1263
	BLKRASET                         = 0x1262
	BLKROGET                         = 0x125e
	BLKROSET                         = 0x125d
	BLKROTATIONAL                    = 0x127e
	BLKRRPART                        = 0x125f
	BLKSECDISCARD                    = 0x127d
	BLKSECTGET                       = 0x1267
	BLKSECTSET                       = 0x1266
	BLKSSZGET                        = 0x1268
	BLKZEROOUT                       = 0x127f
	BOTHER                           = 0x1000
	BS1                              = 0x2000
	BSDLY                            = 0x2000
	CBAUD                            = 0x100f
	CBAUDEX                          = 0x1000
	CIBAUD                           = 0x100f0000
	CLOCAL                           = 0x800
	CR1                              = 0x200
	CR2                              = 0x400
	CR3                              = 0x600
	CRDLY                            = 0x600
	CREAD                            = 0x80
	CS6                              = 0x10
	CS7                              = 0x20
	CS8                              = 0x30
	CSIZE                            = 0x30
	CSTOPB                           = 0x40
	DM_MPATH_PROBE_PATHS             = 0xfd12
	ECCGETLAYOUT                     = 0x81484d11
	ECCGETSTATS                      = 0x80104d12
	ECHOCTL                          = 0x200
	ECHOE                            = 0x10
	ECHOK                            = 0x20
	ECHOKE                           = 0x800
	ECHONL                           = 0x40
	ECHOPRT                          = 0x400
	EFD_CLOEXEC                      = 0x80000
	EFD_NONBLOCK                     = 0x800
	EPIOCGPARAMS                     = 0x80088a02
	EPIOCSPARAMS                     = 0x40088a01
	EPOLL_CLOEXEC                    = 0x80000
	EXTPROC                          = 0x10000
	FF1                              = 0x8000
	FFDLY                            = 0x8000
	FICLONE                          = 0x40049409
	FICLONERANGE                     = 0x4020940d
	FLUSHO                           = 0x1000
	FP_XSTATE_MAGIC2                 = 0x46505845
	FS_IOC_ENABLE_VERITY             = 0x40806685
	FS_IOC_GETFLAGS                  = 0x80086601
	FS_IOC_GET_ENCRYPTION_NONCE      = 0x8010661b
	FS_IOC_GET_ENCRYPTION_POLICY     = 0x400c6615
	FS_IOC_GET_ENCRYPTION_PWSALT     = 0x40106614
	FS_IOC_SETFLAGS                  = 0x40086602
	FS_IOC_SET_ENCRYPTION_POLICY     = 0x800c6613
	F_GETLK                          = 0x5
	F_GETLK64                        = 0x5
	F_GETOWN                         = 0x9
	F_RDLCK                          = 0x0
	F_SETLK                          = 0x6
	F_SETLK64                        = 0x6
	F_SETLKW                         = 0x7
	F_SETLKW64                       = 0x7
	F_SETOWN                         = 0x8
	F_UNLCK                          = 0x2
	F_WRLCK                          = 0x1
	HIDIOCGRAWINFO                   = 0x80084803
	HIDIOCGRDESC                     = 0x90044802
	HIDIOCGRDESCSIZE                 = 0x80044801
	HIDIOCREVOKE                     = 0x4004480d
	HUPCL                            = 0x400
	ICANON                           = 0x2
	IEXTEN                           = 0x8000
	IN_CLOEXEC                       = 0x80000
	IN_NONBLOCK                      = 0x800
	IOCTL_VM_SOCKETS_GET_LOCAL_CID   = 0x7b9
	IPV6_FLOWINFO_MASK               = 0xffffff0f
	IPV6_FLOWLABEL_MASK              = 0xffff0f00
	ISIG                             = 0x1
	IUCLC                            = 0x200
	IXOFF                            = 0x1000
	IXON                             = 0x400
	MAP_32BIT                        = 0x40
	MAP_ABOVE4G                      = 0x80
	MAP_ANON                         = 0x20
	MAP_ANONYMOUS                    = 0x20
	MAP_DENYWRITE                    = 0x800
	MAP_EXECUTABLE                   = 0x1000
	MAP_GROWSDOWN                    = 0x100
	MAP_HUGETLB                      = 0x40000
	MAP_LOCKED                       = 0x2000
	MAP_NONBLOCK                     = 0x10000
	MAP_NORESERVE                    = 0x4000
	MAP_POPULATE                     = 0x8000
	MAP_STACK                        = 0x20000
	MAP_SYNC                         = 0x80000
	MCL_CURRENT                      = 0x1
	MCL_FUTURE                       = 0x2
	MCL_ONFAULT                      = 0x4
	MEMERASE                         = 0x40084d02
	MEMERASE64                       = 0x40104d14
	MEMGETBADBLOCK                   = 0x40084d0b
	MEMGETINFO                       = 0x80204d01
	MEMGETOOBSEL                     = 0x80c84d0a
	MEMGETREGIONCOUNT                = 0x80044d07
	MEMISLOCKED                      = 0x80084d17
	MEMLOCK                          = 0x40084d05
	MEMREAD                          = 0xc0404d1a
	MEMREADOOB                       = 0xc0104d04
	MEMSETBADBLOCK                   = 0x40084d0c
	MEMUNLOCK                        = 0x40084d06
	MEMWRITEOOB                      = 0xc0104d03
	MTDFILEMODE                      = 0x4d13
	NFDBITS                          = 0x40
	NLDLY                            = 0x100
	NOFLSH                           = 0x80
	NS_GET_MNTNS_ID                  = 0x8008b705
	NS_GET_NSTYPE                    = 0xb703
	NS_GET_OWNER_UID                 = 0xb704
	NS_GET_PARENT                    = 0xb702
	NS_GET_PID_FROM_PIDNS            = 0x8004b706
	NS_GET_PID_IN_PIDNS              = 0x8004b708
	NS_GET_TGID_FROM_PIDNS           = 0x8004b707
	NS_GET_TGID_IN_PIDNS             = 0x8004b709
	NS_GET_USERNS                    = 0xb701
	OLCUC                            = 0x2
	ONLCR                            = 0x4
	OTPERASE                         = 0x400c4d19
	OTPGETREGIONCOUNT                = 0x40044d0e
	OTPGETREGIONINFO                 = 0x400c4d0f
	OTPLOCK                          = 0x800c4d10
	OTPSELECT                        = 0x80044d0d
	O_APPEND                         = 0x400
	O_ASYNC                          = 0x2000
	O_CLOEXEC                        = 0x80000
	O_CREAT                          = 0x40
	O_DIRECT                         = 0x4000
	O_DIRECTORY                      = 0x10000
	O_DSYNC                          = 0x1000
	O_EXCL                           = 0x80
	O_FSYNC                          = 0x101000
	O_LARGEFILE                      = 0x0
	O_NDELAY                         = 0x800
	O_NOATIME                        = 0x40000
	O_NOCTTY                         = 0x100
	O_NOFOLLOW                       = 0x20000
	O_NONBLOCK                       = 0x800
	O_PATH                           = 0x200000
	O_RSYNC                          = 0x101000
	O_SYNC                           = 0x101000
	O_TMPFILE                        = 0x410000
	O_TRUNC                          = 0x200
	PARENB                           = 0x100
	PARODD                           = 0x200
	PENDIN                           = 0x4000
	PERF_EVENT_IOC_DISABLE           = 0x2401
	PERF_EVENT_IOC_ENABLE            = 0x2400
	PERF_EVENT_IOC_ID                = 0x80082407
	PERF_EVENT_IOC_MODIFY_ATTRIBUTES = 0x4008240b
	PERF_EVENT_IOC_PAUSE_OUTPUT      = 0x40042409
	PERF_EVENT_IOC_PERIOD            = 0x40082404
	PERF_EVENT_IOC_QUERY_BPF         = 0xc008240a
	PERF_EVENT_IOC_REFRESH           = 0x2402
	PERF_EVENT_IOC_RESET             = 0x2403
	PERF_EVENT_IOC_SET_BPF           = 0x40042408
	PERF_EVENT_IOC_SET_FILTER        = 0x40082406
	PERF_EVENT_IOC_SET_OUTPUT        = 0x2405
	PPPIOCATTACH                     = 0x4004743d
	PPPIOCATTCHAN                    = 0x40047438
	PPPIOCBRIDGECHAN                 = 0x40047435
	PPPIOCCONNECT                    = 0x4004743a
	PPPIOCDETACH                     = 0x4004743c
	PPPIOCDISCONN                    = 0x7439
	PPPIOCGASYNCMAP                  = 0x80047458
	PPPIOCGCHAN                      = 0x80047437
	PPPIOCGDEBUG                     = 0x80047441
	PPPIOCGFLAGS                     = 0x8004745a
	PPPIOCGIDLE                      = 0x8010743f
	PPPIOCGIDLE32                    = 0x8008743f
	PPPIOCGIDLE64                    = 0x8010743f
	PPPIOCGL2TPSTATS                 = 0x80487436
	PPPIOCGMRU                       = 0x80047453
	PPPIOCGRASYNCMAP                 = 0x80047455
	PPPIOCGUNIT                      = 0x80047456
	PPPIOCGXASYNCMAP                 = 0x80207450
	PPPIOCSACTIVE                    = 0x40107446
	PPPIOCSASYNCMAP                  = 0x40047457
	PPPIOCSCOMPRESS                  = 0x4010744d
	PPPIOCSDEBUG                     = 0x40047440
	PPPIOCSFLAGS                     = 0x40047459
	PPPIOCSMAXCID                    = 0x40047451
	PPPIOCSMRRU                      = 0x4004743b
	PPPIOCSMRU                       = 0x40047452
	PPPIOCSNPMODE                    = 0x4008744b
	PPPIOCSPASS                      = 0x40107447
	PPPIOCSRASYNCMAP                 = 0x40047454
	PPPIOCSXASYNCMAP                 = 0x4020744f
	PPPIOCUNBRIDGECHAN               = 0x7434
	PPPIOCXFERUNIT                   = 0x744e
	PR_SET_PTRACER_ANY               = 0xffffffffffffffff
	PTP_CLOCK_GETCAPS                = 0x80503d01
	PTP_CLOCK_GETCAPS2               = 0x80503d0a
	PTP_ENABLE_PPS                   = 0x40043d04
	PTP_ENABLE_PPS2                  = 0x40043d0d
	PTP_EXTTS_REQUEST                = 0x40103d02
	PTP_EXTTS_REQUEST2               = 0x40103d0b
	PTP_MASK_CLEAR_ALL               = 0x3d13
	PTP_MASK_EN_SINGLE               = 0x40043d14
	PTP_PEROUT_REQUEST               = 0x40383d03
	PTP_PEROUT_REQUEST2              = 0x40383d0c
	PTP_PIN_SETFUNC                  = 0x40603d07
	PTP_PIN_SETFUNC2                 = 0x40603d10
	PTP_SYS_OFFSET                   = 0x43403d05
	PTP_SYS_OFFSET2                  = 0x43403d0e
	PTRACE_ARCH_PRCTL                = 0x1e
	PTRACE_GETFPREGS                 = 0xe
	PTRACE_GETFPXREGS                = 0x12
	PTRACE_GET_THREAD_AREA           = 0x19
	PTRACE_OLDSETOPTIONS             = 0x15
	PTRACE_SETFPREGS                 = 0xf
	PTRACE_SETFPXREGS                = 0x13
	PTRACE_SET_THREAD_AREA           = 0x1a
	PTRACE_SINGLEBLOCK               = 0x21
	PTRACE_SYSEMU                    = 0x1f
	PTRACE_SYSEMU_SINGLESTEP         = 0x20
	RLIMIT_AS                        = 0x9
	RLIMIT_MEMLOCK                   = 0x8
	RLIMIT_NOFILE                    = 0x7
	RLIMIT_NPROC                     = 0x6
	RLIMIT_RSS                       = 0x5
	RNDADDENTROPY                    = 0x40085203
	RNDADDTOENTCNT                   = 0x40045201
	RNDCLEARPOOL                     = 0x5206
	RNDGETENTCNT                     = 0x80045200
	RNDGETPOOL                       = 0x80085202
	RNDRESEEDCRNG                    = 0x5207
	RNDZAPENTCNT                     = 0x5204
	RTC_AIE_OFF                      = 0x7002
	RTC_AIE_ON                       = 0x7001
	RTC_ALM_READ                     = 0x80247008
	RTC_ALM_SET                      = 0x40247007
	RTC_EPOCH_READ                   = 0x8008700d
	RTC_EPOCH_SET                    = 0x4008700e
	RTC_IRQP_READ                    = 0x8008700b
	RTC_IRQP_SET                     = 0x4008700c
	RTC_PARAM_GET                    = 0x40187013
	RTC_PARAM_SET                    = 0x40187014
	RTC_PIE_OFF                      = 0x7006
	RTC_PIE_ON                       = 0x7005
	RTC_PLL_GET                      = 0x80207011
	RTC_PLL_SET                      = 0x40207012
	RTC_RD_TIME                      = 0x80247009
	RTC_SET_TIME                     = 0x4024700a
	RTC_UIE_OFF                      = 0x7004
	RTC_UIE_ON                       = 0x7003
	RTC_VL_CLR                       = 0x7014
	RTC_VL_READ                      = 0x80047013
	RTC_WIE_OFF                      = 0x7010
	RTC_WIE_ON                       = 0x700f
	RTC_WKALM_RD                     = 0x80287010
	RTC_WKALM_SET                    = 0x4028700f
	SCM_DEVMEM_DMABUF                = 0x4f
	SCM_DEVMEM_LINEAR                = 0x4e
	SCM_TIMESTAMPING                 = 0x25
	SCM_TIMESTAMPING_OPT_STATS       = 0x36
	SCM_TIMESTAMPING_PKTINFO         = 0x3a
	SCM_TIMESTAMPNS                  = 0x23
	SCM_TS_OPT_ID                    = 0x51
	SCM_TXTIME                       = 0x3d
	SCM_WIFI_STATUS                  = 0x29
	SECCOMP_IOCTL_NOTIF_ADDFD        = 0x40182103
	SECCOMP_IOCTL_NOTIF_ID_VALID     = 0x40082102
	SECCOMP_IOCTL_NOTIF_SET_FLAGS    = 0x40082104
	SFD_CLOEXEC                      = 0x80000
	SFD_NONBLOCK                     = 0x800
	SIOCATMARK                       = 0x8905
	SIOCGPGRP                        = 0x8904
	SIOCGSTAMPNS_NEW                 = 0x80108907
	SIOCGSTAMP_NEW                   = 0x80108906
	SIOCINQ                          = 0x541b
	SIOCOUTQ                         = 0x5411
	SIOCSPGRP                        = 0x8902
	SOCK_CLOEXEC                     = 0x80000
	SOCK_DGRAM                       = 0x2
	SOCK_NONBLOCK                    = 0x800
	SOCK_STREAM                      = 0x1
	SOL_SOCKET                       = 0x1
	SO_ACCEPTCONN                    = 0x1e
	SO_ATTACH_BPF                    = 0x32
	SO_ATTACH_REUSEPORT_CBPF         = 0x33
	SO_ATTACH_REUSEPORT_EBPF         = 0x34
	SO_BINDTODEVICE                  = 0x19
	SO_BINDTOIFINDEX                 = 0x3e
	SO_BPF_EXTENSIONS                = 0x30
	SO_BROADCAST                     = 0x6
	SO_BSDCOMPAT                     = 0xe
	SO_BUF_LOCK                      = 0x48
	SO_BUSY_POLL                     = 0x2e
	SO_BUSY_POLL_BUDGET              = 0x46
	SO_CNX_ADVICE                    = 0x35
	SO_COOKIE                        = 0x39
	SO_DETACH_REUSEPORT_BPF          = 0x44
	SO_DEVMEM_DMABUF                 = 0x4f
	SO_DEVMEM_DONTNEED               = 0x50
	SO_DEVMEM_LINEAR                 = 0x4e
	SO_DOMAIN                        = 0x27
	SO_DONTROUTE                     = 0x5
	SO_ERROR                         = 0x4
	SO_INCOMING_CPU                  = 0x31
	SO_INCOMING_NAPI_ID              = 0x38
	SO_KEEPALIVE                     = 0x9
	SO_LINGER                        = 0xd
	SO_LOCK_FILTER                   = 0x2c
	SO_MARK                          = 0x24
	SO_MAX_PACING_RATE               = 0x2f
	SO_MEMINFO                       = 0x37
	SO_NETNS_COOKIE                  = 0x47
	SO_NOFCS                         = 0x2b
	SO_OOBINLINE                     = 0xa
	SO_PASSCRED                      = 0x10
	SO_PASSPIDFD                     = 0x4c
	SO_PASSRIGHTS                    = 0x53
	SO_PASSSEC                       = 0x22
	SO_PEEK_OFF                      = 0x2a
	SO_PEERCRED                      = 0x11
	SO_PEERGROUPS                    = 0x3b
	SO_PEERPIDFD                     = 0x4d
	SO_PEERSEC                       = 0x1f
	SO_PREFER_BUSY_POLL              = 0x45
	SO_PROTOCOL                      = 0x26
	SO_RCVBUF                        = 0x8
	SO_RCVBUFFORCE                   = 0x21
	SO_RCVLOWAT                      = 0x12
	SO_RCVMARK                       = 0x4b
	SO_RCVPRIORITY                   = 0x52
	SO_RCVTIMEO                      = 0x14
	SO_RCVTIMEO_NEW                  = 0x42
	SO_RCVTIMEO_OLD                  = 0x14
	SO_RESERVE_MEM                   = 0x49
	SO_REUSEADDR                     = 0x2
	SO_REUSEPORT                     = 0xf
	SO_RXQ_OVFL                      = 0x28
	SO_SECURITY_AUTHENTICATION       = 0x16
	SO_SECURITY_ENCRYPTION_NETWORK   = 0x18
	SO_SECURITY_ENCRYPTION_TRANSPORT = 0x17
	SO_SELECT_ERR_QUEUE              = 0x2d
	SO_SNDBUF                        = 0x7
	SO_SNDBUFFORCE                   = 0x20
	SO_SNDLOWAT                      = 0x13
	SO_SNDTIMEO                      = 0x15
	SO_SNDTIMEO_NEW                  = 0x43
	SO_SNDTIMEO_OLD                  = 0x15
	SO_TIMESTAMPING                  = 0x25
	SO_TIMESTAMPING_NEW              = 0x41
	SO_TIMESTAMPING_OLD              = 0x25
	SO_TIMESTAMPNS                   = 0x23
	SO_TIMESTAMPNS_NEW               = 0x40
	SO_TIMESTAMPNS_OLD               = 0x23
	SO_TIMESTAMP_NEW                 = 0x3f
	SO_TXREHASH                      = 0x4a
	SO_TXTIME                        = 0x3d
	SO_TYPE                          = 0x3
	SO_WIFI_STATUS                   = 0x29
	SO_ZEROCOPY                      = 0x3c
	TAB1                             = 0x800
	TAB2                             = 0x1000
	TAB3                             = 0x1800
	TABDLY                           = 0x1800
	TCFLSH                           = 0x540b
	TCGETA                           = 0x5405
	TCGETS                           = 0x5401
	TCGETS2                          = 0x802c542a
	TCGETX                           = 0x5432
	TCSAFLUSH                        = 0x2
	TCSBRK                           = 0x5409
	TCSBRKP                          = 0x5425
	TCSETA                           = 0x5406
	TCSETAF                          = 0x5408
	TCSETAW                          = 0x5407
	TCSETS                           = 0x5402
	TCSETS2                          = 0x402c542b
	TCSETSF                          = 0x5404
	TCSETSF2                         = 0x402c542d
	TCSETSW                          = 0x5403
	TCSETSW2                         = 0x402c542c
	TCSETX                           = 0x5433
	TCSETXF                          = 0x5434
	TCSETXW                          = 0x5435
	TCXONC                           = 0x540a
	TFD_CLOEXEC                      = 0x80000
	TFD_NONBLOCK                     = 0x800
	TIOCCBRK                         = 0x5428
	TIOCCONS                         = 0x541d
	TIOCEXCL                         = 0x540c
	TIOCGDEV                         = 0x80045432
	TIOCGETD                         = 0x5424
	TIOCGEXCL                        = 0x80045440
	TIOCGICOUNT                      = 0x545d
	TIOCGISO7816                     = 0x80285442
	TIOCGLCKTRMIOS                   = 0x5456
	TIOCGPGRP                        = 0x540f
	TIOCGPKT                         = 0x80045438
	TIOCGPTLCK                       = 0x80045439
	TIOCGPTN                         = 0x80045430
	TIOCGPTPEER                      = 0x5441
	TIOCGRS485                       = 0x542e
	TIOCGSERIAL                      = 0x541e
	TIOCGSID                         = 0x5429
	TIOCGSOFTCAR                     = 0x5419
	TIOCGWINSZ                       = 0x5413
	TIOCINQ                          = 0x541b
	TIOCLINUX                        = 0x541c
	TIOCMBIC                         = 0x5417
	TIOCMBIS                         = 0x5416
	TIOCMGET                         = 0x5415
	TIOCMIWAIT                       = 0x545c
	TIOCMSET                         = 0x5418
	TIOCM_CAR                        = 0x40
	TIOCM_CD                         = 0x40
	TIOCM_CTS                        = 0x20
	TIOCM_DSR                        = 0x100
	TIOCM_RI                         = 0x80
	TIOCM_RNG                        = 0x80
	TIOCM_SR                         = 0x10
	TIOCM_ST                         = 0x8
	TIOCNOTTY                        = 0x5422
	TIOCNXCL                         = 0x540d
	TIOCOUTQ                         = 0x5411
	TIOCPKT                          = 0x5420
	TIOCSBRK                         = 0x5427
	TIOCSCTTY                        = 0x540e
	TIOCSERCONFIG                    = 0x5453
	TIOCSERGETLSR                    = 0x5459
	TIOCSERGETMULTI                  = 0x545a
	TIOCSERGSTRUCT                   = 0x5458
	TIOCSERGWILD                     = 0x5454
	TIOCSERSETMULTI                  = 0x545b
	TIOCSERSWILD                     = 0x5455
	TIOCSER_TEMT                     = 0x1
	TIOCSETD                         = 0x5423
	TIOCSIG                          = 0x40045436
	TIOCSISO7816                     = 0xc0285443
	TIOCSLCKTRMIOS                   = 0x5457
	TIOCSPGRP                        = 0x5410
	TIOCSPTLCK                       = 0x40045431
	TIOCSRS485                       = 0x542f
	TIOCSSERIAL                      = 0x541f
	TIOCSSOFTCAR                     = 0x541a
	TIOCSTI                          = 0x5412
	TIOCSWINSZ                       = 0x5414
	TIOCVHANGUP                      = 0x5437
	TOSTOP                           = 0x100
	TUNATTACHFILTER                  = 0x401054d5
	TUNDETACHFILTER                  = 0x401054d6
	TUNGETDEVNETNS                   = 0x54e3
	TUNGETFEATURES                   = 0x800454cf
	TUNGETFILTER                     = 0x801054db
	TUNGETIFF                        = 0x800454d2
	TUNGETSNDBUF                     = 0x800454d3
	TUNGETVNETBE                     = 0x800454df
	TUNGETVNETHDRSZ                  = 0x800454d7
	TUNGETVNETLE                     = 0x800454dd
	TUNSETCARRIER                    = 0x400454e2
	TUNSETDEBUG                      = 0x400454c9
	TUNSETFILTEREBPF                 = 0x800454e1
	TUNSETGROUP                      = 0x400454ce
	TUNSETIFF                        = 0x400454ca
	TUNSETIFINDEX                    = 0x400454da
	TUNSETLINK                       = 0x400454cd
	TUNSETNOCSUM                     = 0x400454c8
	TUNSETOFFLOAD                    = 0x400454d0
	TUNSETOWNER                      = 0x400454cc
	TUNSETPERSIST                    = 0x400454cb
	TUNSETQUEUE                      = 0x400454d9
	TUNSETSNDBUF                     = 0x400454d4
	TUNSETSTEERINGEBPF               = 0x800454e0
	TUNSETTXFILTER                   = 0x400454d1
	TUNSETVNETBE                     = 0x400454de
	TUNSETVNETHDRSZ                  = 0x400454d8
	TUNSETVNETLE                     = 0x400454dc
	UBI_IOCATT                       = 0x40186f40
	UBI_IOCDET                       = 0x40046f41
	UBI_IOCEBCH                      = 0x40044f02
	UBI_IOCEBER                      = 0x40044f01
	UBI_IOCEBISMAP                   = 0x80044f05
	UBI_IOCEBMAP                     = 0x40084f03
	UBI_IOCEBUNMAP                   = 0x40044f04
	UBI_IOCMKVOL                     = 0x40986f00
	UBI_IOCRMVOL                     = 0x40046f01
	UBI_IOCRNVOL                     = 0x51106f03
	UBI_IOCRPEB                      = 0x40046f04
	UBI_IOCRSVOL                     = 0x400c6f02
	UBI_IOCSETVOLPROP                = 0x40104f06
	UBI_IOCSPEB                      = 0x40046f05
	UBI_IOCVOLCRBLK                  = 0x40804f07
	UBI_IOCVOLRMBLK                  = 0x4f08
	UBI_IOCVOLUP                     = 0x40084f00
	VDISCARD                         = 0xd
	VEOF                             = 0x4
	VEOL                             = 0xb
	VEOL2                            = 0x10
	VMIN                             = 0x6
	VREPRINT                         = 0xc
	VSTART                           = 0x8
	VSTOP                            = 0x9
	VSUSP                            = 0xa
	VSWTC                            = 0x7
	VT1                              = 0x4000
	VTDLY                            = 0x4000
	VTIME                            = 0x5
	VWERASE                          = 0xe
	WDIOC_GETBOOTSTATUS              = 0x80045702
	WDIOC_GETPRETIMEOUT              = 0x80045709
	WDIOC_GETSTATUS                  = 0x80045701
	WDIOC_GETSUPPORT                 = 0x80285700
	WDIOC_GETTEMP                    = 0x80045703
	WDIOC_GETTIMELEFT                = 0x8004570a
	WDIOC_GETTIMEOUT                 = 0x80045707
	WDIOC_KEEPALIVE                  = 0x80045705
	WDIOC_SETOPTIONS                 = 0x80045704
	WORDSIZE                         = 0x40
	XCASE                            = 0x4
	XTABS                            = 0x1800
	_HIDIOCGRAWNAME                  = 0x80804804
	_HIDIOCGRAWPHYS                  = 0x80404805
	_HIDIOCGRAWUNIQ                  = 0x80404808
)

// Error table
var errors = [...]string{
	1:   "operation not permitted",
	2:   "no such file or directory",
	3:   "no such process",
	4:   "interrupted system call",
	5:   "input/output error",
	6:   "no such device or address",
	7:   "argument list too long",
	8:   "exec format error",
	9:   "bad file descriptor",
	10:  "no child processes",
	11:  "resource temporarily unavailable",
	12:  "cannot allocate memory",
	13:  "permission denied",
	14:  "bad address",
	15:  "block device required",
	16:  "device or resource busy",
	17:  "file exists",
	18:  "invalid cross-device link",
	19:  "no such device",
	20:  "not a directory",
	21:  "is a directory",
	22:  "invalid argument",
	23:  "too many open files in system",
	24:  "too many open files",
	25:  "inappropriate ioctl for device",
	26:  "text file busy",
	27:  "file too large",
	28:  "no space left on device",
	29:  "illegal seek",
	30:  "read-only file system",
	31:  "too many links",
	32:  "broken pipe",
	33:  "numerical argument out of domain",
	34:  "numerical result out of range",
	35:  "resource deadlock avoided",
	36:  "file name too long",
	37:  "no locks available",
	38:  "function not implemented",
	39:  "directory not empty",
	40:  "too many levels of symbolic links",
	42:  "no message of desired type",
	43:  "identifier removed",
	44:  "channel number out of range",
	45:  "level 2 not synchronized",
	46:  "level 3 halted",
	47:  "level 3 reset",
	48:  "link number out of range",
	49:  "protocol driver not attached",
	50:  "no CSI structure available",
	51:  "level 2 halted",
	52:  "invalid exchange",
	53:  "invalid request descriptor",
	54:  "exchange full",
	55:  "no anode",
	56:  "invalid request code",
	57:  "invalid slot",
	59:  "bad font file format",
	60:  "device not a stream",
	61:  "no data available",
	62:  "timer expired",
	63:  "out of streams resources",
	64:  "machine is not on the network",
	65:  "package not installed",
	66:  "object is remote",
	67:  "link has been severed",
	68:  "advertise error",
	69:  "srmount error",
	70:  "communication error on send",
	71:  "protocol error",
	72:  "multihop attempted",
	73:  "RFS specific error",
	74:  "bad message",
	75:  "value too large for defined data type",
	76:  "name not unique on network",
	77:  "file descriptor in bad state",
	78:  "remote address changed",
	79:  "can not access a needed shared library",
	80:  "accessing a corrupted shared library",
	81:  ".lib section in a.out corrupted",
	82:  "attempting to link in too many shared libraries",
	83:  "cannot exec a shared library directly",
	84:  "invalid or incomplete multibyte or wide character",
	85:  "interrupted system call should be restarted",
	86:  "streams pipe error",
	87:  "too many users",
	88:  "socket operation on non-socket",
	89:  "destination address required",
	90:  "message too long",
	91:  "protocol wrong type for socket",
	92:  "protocol not available",
	93:  "protocol not supported",
	94:  "socket type not supported",
	95:  "operation not supported",
	96:  "protocol family not supported",
	97:  "address family not supported by protocol",
	98:  "address already in use",
	99:  "cannot assign requested address",
	100: "network is down",
	101: "network is unreachable",
	102: "network dropped connection on reset",
	103: "software caused connection abort",
	104: "connection reset by peer",
	105: "no buffer space available",
	106: "transport endpoint is already connected",
	107: "transport endpoint is not connected",
	108: "cannot send after transport endpoint shutdown",
	109: "too many references: cannot splice",
	110: "connection timed out",
	111: "connection refused",
	112: "host is down",
	113: "no route to host",
	114: "operation already in progress",
	115: "operation now in progress",
	116: "stale file handle",
	117: "structure needs cleaning",
	118: "not a XENIX named type file",
	119: "no XENIX semaphores available",
	120: "is a named type file",
	121: "remote I/O error",
	122: "disk quota exceeded",
	123: "no medium found",
	124: "wrong medium type",
	125: "operation canceled",
	126: "required key not available",
	127: "key has expired",
	128: "key has been revoked",
	129: "key was rejected by service",
	130: "owner died",
	131: "state not recoverable",
	132: "operation not possible due to RF-kill",
}

// Signal table
var signals = [...]string{
	1:  "hangup",
	2:  "interrupt",
	3:  "quit",
	4:  "illegal instruction",
	5:  "trace/breakpoint trap",
	6:  "aborted",
	7:  "bus error",
	8:  "floating point exception",
	9:  "killed",
	10: "user defined signal 1",
	11: "segmentation fault",
	12: "user defined signal 2",
	13: "broken pipe",
	14: "alarm clock",
	15: "terminated",
	16: "stack fault",
	17: "child exited",
	18: "continued",
	19: "stopped (signal)",
	20: "stopped",
	21: "stopped (tty input)",
	22: "stopped (tty output)",
	23: "urgent I/O condition",
	24: "CPU time limit exceeded",
	25: "file size limit exceeded",
	26: "virtual timer expired",
	27: "profiling timer expired",
	28: "window changed",
	29: "I/O possible",
	30: "power failure",
	31: "bad system call",
}

type Errno int

func (e Errno) Error() string {
	if 0 <= int(e) && int(e) < len(errors) {
		s := errors[e]
		if s != "" {
			return s
		}
	}
	return "errno " + strconv.Itoa(int(e))
}

const (
	E2BIG       = Errno(0x7)
	EACCES      = Errno(0xd)
	EAGAIN      = Errno(0xb)
	EBADF       = Errno(0x9)
	EBUSY       = Errno(0x10)
	ECHILD      = Errno(0xa)
	EDOM        = Errno(0x21)
	EEXIST      = Errno(0x11)
	EFAULT      = Errno(0xe)
	EFBIG       = Errno(0x1b)
	EINTR       = Errno(0x4)
	EINVAL      = Errno(0x16)
	EIO         = Errno(0x5)
	EISDIR      = Errno(0x15)
	EMFILE      = Errno(0x18)
	EMLINK      = Errno(0x1f)
	ENFILE      = Errno(0x17)
	ENODEV      = Errno(0x13)
	ENOENT      = Errno(0x2)
	ENOEXEC     = Errno(0x8)
	ENOMEM      = Errno(0xc)
	ENOSPC      = Errno(0x1c)
	ENOTBLK     = Errno(0xf)
	ENOTDIR     = Errno(0x14)
	ENOTTY      = Errno(0x19)
	ENXIO       = Errno(0x6)
	EPERM       = Errno(0x1)
	EPIPE       = Errno(0x20)
	ERANGE      = Errno(0x22)
	EROFS       = Errno(0x1e)
	ESPIPE      = Errno(0x1d)
	ESRCH       = Errno(0x3)
	ETXTBSY     = Errno(0x1a)
	EWOULDBLOCK = Errno(0xb)
	EXDEV       = Errno(0x12)
	ENOSYS      = Errno(0x26)
)
