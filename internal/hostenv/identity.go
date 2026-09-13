package hostenv

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
)

// Passwd returns /etc/passwd contents for a container running as the host user.
func Passwd(username string, uid, gid int, home string) string {
	username = safeUsername(username)
	if strings.ContainsAny(home, ":\r\n") {
		home = "/tmp"
	}

	var contents strings.Builder
	contents.WriteString("root:x:0:0:root:/root:/bin/bash\n")
	if uid != 0 && uid != 65534 {
		fmt.Fprintf(&contents, "%s:x:%d:%d:%s:%s:/bin/bash\n", username, uid, gid, username, home)
	}
	contents.WriteString("nobody:x:65534:65534:nobody:/nonexistent:/usr/sbin/nologin\n")
	return contents.String()
}

// Group returns /etc/group contents; extra maps group names to gids the user is also a member of.
func Group(username string, gid int, extra map[string]int) string {
	username = safeUsername(username)
	groups := []groupEntry{{name: "root", gid: 0}}
	byGID := map[int]int{0: 0}
	if gid != 0 && gid != 65534 {
		byGID[gid] = len(groups)
		groups = append(groups, groupEntry{name: username, gid: gid})
	}
	byGID[65534] = len(groups)
	groups = append(groups, groupEntry{name: "nogroup", gid: 65534})

	names := make([]string, 0, len(extra))
	for name := range extra {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		extraGID := extra[name]
		if index, ok := byGID[extraGID]; ok {
			groups[index].members = appendMember(groups[index].members, username)
			continue
		}
		byGID[extraGID] = len(groups)
		groups = append(groups, groupEntry{
			name:    safeGroupName(name, extraGID),
			gid:     extraGID,
			members: []string{username},
		})
	}

	var contents strings.Builder
	for _, group := range groups {
		fmt.Fprintf(&contents, "%s:x:%d:%s\n", group.name, group.gid, strings.Join(group.members, ","))
	}
	return contents.String()
}

type groupEntry struct {
	name    string
	gid     int
	members []string
}

func safeUsername(name string) string {
	if unsafeName(name) {
		return "dx"
	}
	return name
}

func safeGroupName(name string, gid int) string {
	if unsafeName(name) {
		return fmt.Sprintf("dx-%d", gid)
	}
	return name
}

func unsafeName(name string) bool {
	return name == "" || strings.ContainsAny(name, ":,") || strings.IndexFunc(name, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r)
	}) >= 0
}

func appendMember(members []string, username string) []string {
	for _, member := range members {
		if member == username {
			return members
		}
	}
	return append(members, username)
}
