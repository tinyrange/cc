//go:build linux

package vm

import (
	"fmt"
	"os"
	osuser "os/user"
	"strconv"
	"strings"

	"j5.nz/cc/internal/imagefs"
	"j5.nz/cc/internal/vmruntime"
)

func addLinuxRuntimeIdentityFiles(overlay *imagefs.Overlay, uid, gid int) {
	if overlay == nil {
		return
	}
	identity := linuxRuntimeHostIdentity(uid, gid)
	addLinuxRuntimeIdentityFilesForUser(overlay, identity)
}

type linuxRuntimeIdentity struct {
	Name   string
	UID    int
	GID    int
	Gecos  string
	Home   string
	Shell  string
	Groups []linuxRuntimeGroup
}

type linuxRuntimeGroup struct {
	Name string
	GID  int
}

func linuxRuntimeHostIdentity(uid, gid int) linuxRuntimeIdentity {
	name := "ccx3"
	home := "/home/ccx3"
	gecos := "ccx3 user"
	shell := "/bin/sh"
	if u, err := osuser.LookupId(strconv.Itoa(uid)); err == nil {
		if u.Username != "" {
			name = u.Username
		}
		if u.Name != "" {
			gecos = u.Name
		}
		if u.HomeDir != "" {
			home = u.HomeDir
		}
		if parsed := linuxRuntimeHostPasswdIdentity(uid); parsed.Shell != "" {
			shell = parsed.Shell
			if parsed.Gecos != "" {
				gecos = parsed.Gecos
			}
		}
	}
	groups := []linuxRuntimeGroup{linuxRuntimeHostGroup(gid, name)}
	for _, groupID := range linuxRuntimeHostGroups() {
		if groupID == gid {
			continue
		}
		groups = append(groups, linuxRuntimeHostGroup(groupID, name))
	}
	return linuxRuntimeIdentity{
		Name:   name,
		UID:    uid,
		GID:    gid,
		Gecos:  gecos,
		Home:   home,
		Shell:  shell,
		Groups: groups,
	}
}

func linuxRuntimeHostPasswdIdentity(uid int) linuxRuntimeIdentity {
	passwd, err := os.ReadFile("/etc/passwd")
	if err != nil {
		return linuxRuntimeIdentity{}
	}
	uidText := strconv.Itoa(uid)
	for _, line := range strings.Split(string(passwd), "\n") {
		fields := strings.Split(line, ":")
		if len(fields) >= 7 && fields[2] == uidText {
			return linuxRuntimeIdentity{Gecos: fields[4], Shell: fields[6]}
		}
	}
	return linuxRuntimeIdentity{}
}

func linuxRuntimeHostGroups() []int {
	groupIDs, err := os.Getgroups()
	if err != nil {
		return nil
	}
	return groupIDs
}

func linuxRuntimeHostGroup(gid int, fallbackName string) linuxRuntimeGroup {
	name := fallbackName
	if g, err := osuser.LookupGroupId(strconv.Itoa(gid)); err == nil && g.Name != "" {
		name = g.Name
	}
	return linuxRuntimeGroup{Name: name, GID: gid}
}

func addLinuxRuntimeIdentityFilesForUser(overlay *imagefs.Overlay, identity linuxRuntimeIdentity) {
	if overlay == nil {
		return
	}
	passwd := readLinuxRuntimeTextFile(overlay.Root(), "/etc/passwd")
	group := readLinuxRuntimeTextFile(overlay.Root(), "/etc/group")

	if strings.TrimSpace(passwd) == "" {
		passwd = "root:x:0:0:root:/root:/bin/sh\n"
	}
	if strings.TrimSpace(group) == "" {
		group = "root:x:0:\n"
	}

	_ = overlay.AddFile("/etc/passwd", 0o644, []byte(linuxRuntimePasswdContent(passwd, identity)))
	_ = overlay.AddFile("/etc/group", 0o644, []byte(linuxRuntimeGroupContent(group, identity)))
}

func readLinuxRuntimeTextFile(root imagefs.Directory, guestPath string) string {
	entry, err := imagefs.LookupPath(root, guestPath)
	if err != nil || entry.File == nil {
		return ""
	}
	size, _ := entry.File.Stat()
	if size == 0 {
		return ""
	}
	data, err := entry.File.ReadAt(0, uint32(size))
	if err != nil && len(data) == 0 {
		return ""
	}
	return string(data)
}

func linuxRuntimePasswdContent(passwd string, identity linuxRuntimeIdentity) string {
	lines := strings.Split(strings.TrimSuffix(passwd, "\n"), "\n")
	uidText := strconv.Itoa(identity.UID)
	for i, line := range lines {
		if line == "" {
			continue
		}
		fields := strings.Split(line, ":")
		if len(fields) >= 7 && fields[2] == uidText {
			lines[i] = strings.Join([]string{
				identity.Name,
				"x",
				uidText,
				fields[3],
				identity.Gecos,
				identity.Home,
				fields[6],
			}, ":")
			return strings.Join(append(lines, ""), "\n")
		}
	}
	return ensureTrailingNewline(passwd) + linuxRuntimePasswdLine(identity)
}

func linuxRuntimePasswdLine(identity linuxRuntimeIdentity) string {
	return fmt.Sprintf("%s:x:%d:%d:%s:%s:%s\n", identity.Name, identity.UID, identity.GID, identity.Gecos, identity.Home, identity.Shell)
}

func linuxRuntimeGroupContent(group string, identity linuxRuntimeIdentity) string {
	group = ensureTrailingNewline(group)
	seen := map[string]bool{}
	for _, hostGroup := range identity.Groups {
		line := fmt.Sprintf("%s:x:%d:%s\n", hostGroup.Name, hostGroup.GID, identity.Name)
		if !seen[line] {
			seen[line] = true
			group += line
		}
	}
	return group
}

func ensureTrailingNewline(value string) string {
	if value == "" || strings.HasSuffix(value, "\n") {
		return value
	}
	return value + "\n"
}

func addLinuxRuntimeHostnameFiles(overlay *imagefs.Overlay) {
	hostname := vmruntime.DefaultHostname("")
	_ = overlay.AddFile("/etc/hostname", 0o644, []byte(hostname+"\n"))
	hosts := "127.0.0.1\tlocalhost " + hostname + "\n::1\tlocalhost ip6-localhost ip6-loopback " + hostname + "\n"
	_ = overlay.AddFile("/etc/hosts", 0o644, []byte(hosts))
}
