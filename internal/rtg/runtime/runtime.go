// Package runtime provides stub declarations for the RTG runtime.
// This package is used for Go compiler type checking and IDE completion.
// The actual implementations are provided by the RTG compiler's builtins.
// This package should never be executed at runtime.
package runtime

// GOARCH is resolved at RTG compile time to the target architecture.
// The default value is for IDE completion only.
var GOARCH string = "amd64"

// Builtin function stubs with proper signatures for Go type checking.
// These functions accept any types to allow flexibility in RTG source.

// Syscall executes a system call with the given number and arguments.
func Syscall(num int64, args ...any) int64 { return 0 }

// Printf prints a formatted message to the console.
func Printf(format string, args ...any) {}

// Load8 loads an 8-bit value from memory at ptr+offset.
func Load8(ptr, offset int64) int64 { return 0 }

// Load16 loads a 16-bit value from memory at ptr+offset.
func Load16(ptr, offset int64) int64 { return 0 }

// Load32 loads a 32-bit value from memory at ptr+offset.
func Load32(ptr, offset int64) int64 { return 0 }

// Load64 loads a 64-bit value from memory at ptr+offset.
func Load64(ptr, offset int64) int64 { return 0 }

// Store8 stores an 8-bit value to memory at ptr+offset.
func Store8(ptr, offset, value int64) {}

// Store16 stores a 16-bit value to memory at ptr+offset.
func Store16(ptr, offset, value int64) {}

// Store32 stores a 32-bit value to memory at ptr+offset.
func Store32(ptr, offset, value int64) {}

// Store64 stores a 64-bit value to memory at ptr+offset.
func Store64(ptr, offset, value int64) {}

// Call performs an indirect function call to the given address.
func Call(target int64) int64 { return 0 }

// GotoLabel jumps to the specified label.
func GotoLabel(label int64) {}

// ISB is an instruction synchronization barrier (ARM64).
// Required after modifying code in memory before executing it.
func ISB() {}

// Syscall numbers (architecture-independent definitions for type checking)
const (
	SYS_EXIT          int64 = 0
	SYS_EXIT_GROUP    int64 = 1
	SYS_WRITE         int64 = 2
	SYS_READ          int64 = 3
	SYS_OPENAT        int64 = 4
	SYS_CLOSE         int64 = 5
	SYS_MMAP          int64 = 6
	SYS_MUNMAP        int64 = 7
	SYS_MOUNT         int64 = 8
	SYS_MKDIRAT       int64 = 9
	SYS_MKNODAT       int64 = 10
	SYS_CHROOT        int64 = 11
	SYS_CHDIR         int64 = 12
	SYS_SETHOSTNAME   int64 = 13
	SYS_IOCTL         int64 = 14
	SYS_DUP3          int64 = 15
	SYS_SETSID        int64 = 16
	SYS_REBOOT        int64 = 17
	SYS_INIT_MODULE   int64 = 18
	SYS_CLOCK_SETTIME int64 = 19
	SYS_CLOCK_GETTIME int64 = 20
	SYS_SOCKET        int64 = 21
	SYS_SENDTO        int64 = 22
	SYS_RECVFROM      int64 = 23
	SYS_EXECVE        int64 = 24
	SYS_CLONE         int64 = 25
	SYS_WAIT4         int64 = 26
	SYS_MPROTECT      int64 = 27
	SYS_GETPID        int64 = 28
)

// File descriptor constants
const (
	AT_FDCWD int64 = -100
)

// File mode constants
const (
	S_IFCHR int64 = 0o020000
)

// File open flags
const (
	O_RDONLY int64 = 0
	O_WRONLY int64 = 1
	O_RDWR   int64 = 2
	O_CREAT  int64 = 0o100
	O_TRUNC  int64 = 0o1000
	O_SYNC   int64 = 0o4010000
)

// Memory protection flags
const (
	PROT_READ  int64 = 1
	PROT_WRITE int64 = 2
	PROT_EXEC  int64 = 4
)

// Memory map flags
const (
	MAP_SHARED    int64 = 1
	MAP_PRIVATE   int64 = 2
	MAP_ANONYMOUS int64 = 0x20
)

// Error numbers (as negative values for syscall returns)
const (
	EBUSY  int64 = -16
	EPERM  int64 = -1
	EEXIST int64 = -17
	EPIPE  int64 = -32
)

// Reboot magic numbers and commands
const (
	LINUX_REBOOT_MAGIC1        int64 = 0xfee1dead
	LINUX_REBOOT_MAGIC2        int64 = 672274793
	LINUX_REBOOT_CMD_RESTART   int64 = 0x01234567
	LINUX_REBOOT_CMD_POWER_OFF int64 = 0x4321fedc
)

// TTY ioctl constants
const (
	TIOCSCTTY int64 = 0x540E
)

// Clock constants
const (
	CLOCK_REALTIME int64 = 0
)

// Network constants
const (
	AF_INET       int64 = 2
	AF_NETLINK    int64 = 16
	SOCK_DGRAM    int64 = 2
	SOCK_RAW      int64 = 3
	NETLINK_ROUTE int64 = 0
)
