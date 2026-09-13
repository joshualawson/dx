package hostenv

import "testing"

func TestPasswd(t *testing.T) {
	tests := []struct {
		name     string
		username string
		uid      int
		gid      int
		home     string
		want     string
	}{
		{
			name: "normal user", username: "josh", uid: 1000, gid: 1000, home: "/home/josh",
			want: "root:x:0:0:root:/root:/bin/bash\n" +
				"josh:x:1000:1000:josh:/home/josh:/bin/bash\n" +
				"nobody:x:65534:65534:nobody:/nonexistent:/usr/sbin/nologin\n",
		},
		{
			name: "root uid", username: "josh", uid: 0, gid: 0, home: "/home/josh",
			want: "root:x:0:0:root:/root:/bin/bash\n" +
				"nobody:x:65534:65534:nobody:/nonexistent:/usr/sbin/nologin\n",
		},
		{
			name: "nobody uid", username: "josh", uid: 65534, gid: 65534, home: "/home/josh",
			want: "root:x:0:0:root:/root:/bin/bash\n" +
				"nobody:x:65534:65534:nobody:/nonexistent:/usr/sbin/nologin\n",
		},
		{
			name: "unsafe username", username: "bad,user", uid: 1001, gid: 1002, home: "/home/user",
			want: "root:x:0:0:root:/root:/bin/bash\n" +
				"dx:x:1001:1002:dx:/home/user:/bin/bash\n" +
				"nobody:x:65534:65534:nobody:/nonexistent:/usr/sbin/nologin\n",
		},
		{
			name: "unsafe home colon", username: "user", uid: 1001, gid: 1002, home: "/home:other",
			want: "root:x:0:0:root:/root:/bin/bash\n" +
				"user:x:1001:1002:user:/tmp:/bin/bash\n" +
				"nobody:x:65534:65534:nobody:/nonexistent:/usr/sbin/nologin\n",
		},
		{
			name: "unsafe home newline", username: "user", uid: 1001, gid: 1002, home: "/home/user\nroot",
			want: "root:x:0:0:root:/root:/bin/bash\n" +
				"user:x:1001:1002:user:/tmp:/bin/bash\n" +
				"nobody:x:65534:65534:nobody:/nonexistent:/usr/sbin/nologin\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Passwd(tt.username, tt.uid, tt.gid, tt.home); got != tt.want {
				t.Errorf("Passwd(%q, %d, %d, %q) = %q, want %q", tt.username, tt.uid, tt.gid, tt.home, got, tt.want)
			}
		})
	}
}

func TestGroup(t *testing.T) {
	tests := []struct {
		name     string
		username string
		gid      int
		extra    map[string]int
		want     string
	}{
		{
			name: "normal user", username: "josh", gid: 1000,
			want: "root:x:0:\n" +
				"josh:x:1000:\n" +
				"nogroup:x:65534:\n",
		},
		{
			name: "root primary group collision", username: "josh", gid: 0,
			want: "root:x:0:\n" +
				"nogroup:x:65534:\n",
		},
		{
			name: "nogroup primary group collision", username: "josh", gid: 65534,
			want: "root:x:0:\n" +
				"nogroup:x:65534:\n",
		},
		{
			name: "docker extra group", username: "josh", gid: 1000, extra: map[string]int{"docker": 999},
			want: "root:x:0:\n" +
				"josh:x:1000:\n" +
				"nogroup:x:65534:\n" +
				"docker:x:999:josh\n",
		},
		{
			name: "extra collides with primary", username: "josh", gid: 1000, extra: map[string]int{"docker": 1000},
			want: "root:x:0:\n" +
				"josh:x:1000:josh\n" +
				"nogroup:x:65534:\n",
		},
		{
			name: "extras collide with builtins", username: "josh", gid: 1000, extra: map[string]int{"admin": 0, "nobody": 65534},
			want: "root:x:0:josh\n" +
				"josh:x:1000:\n" +
				"nogroup:x:65534:josh\n",
		},
		{
			name: "same extra gid", username: "josh", gid: 1000, extra: map[string]int{"first": 2000, "second": 2000},
			want: "root:x:0:\n" +
				"josh:x:1000:\n" +
				"nogroup:x:65534:\n" +
				"first:x:2000:josh\n",
		},
		{
			name: "unsafe username and group names", username: "bad user", gid: 1000, extra: map[string]int{"": 2000, "bad:group": 2001, "bad,group": 2002, "bad\tgroup": 2003, "bad\x00group": 2004},
			want: "root:x:0:\n" +
				"dx:x:1000:\n" +
				"nogroup:x:65534:\n" +
				"dx-2000:x:2000:dx\n" +
				"dx-2004:x:2004:dx\n" +
				"dx-2003:x:2003:dx\n" +
				"dx-2002:x:2002:dx\n" +
				"dx-2001:x:2001:dx\n",
		},
		{
			name: "deterministic order", username: "josh", gid: 1000, extra: map[string]int{"zebra": 2003, "alpha": 2001, "middle": 2002},
			want: "root:x:0:\n" +
				"josh:x:1000:\n" +
				"nogroup:x:65534:\n" +
				"alpha:x:2001:josh\n" +
				"middle:x:2002:josh\n" +
				"zebra:x:2003:josh\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for range 20 {
				if got := Group(tt.username, tt.gid, tt.extra); got != tt.want {
					t.Errorf("Group(%q, %d, %#v) = %q, want %q", tt.username, tt.gid, tt.extra, got, tt.want)
				}
			}
		})
	}
}

func TestUnsafeIdentityNames(t *testing.T) {
	tests := []string{"", "bad:name", "bad,name", "bad name", "bad\tname", "bad\nname", "bad\x00name", "bad\u2003name"}
	for _, name := range tests {
		t.Run(name, func(t *testing.T) {
			wantPasswd := "root:x:0:0:root:/root:/bin/bash\n" +
				"dx:x:1000:1000:dx:/home/user:/bin/bash\n" +
				"nobody:x:65534:65534:nobody:/nonexistent:/usr/sbin/nologin\n"
			if got := Passwd(name, 1000, 1000, "/home/user"); got != wantPasswd {
				t.Errorf("Passwd(%q, ...) = %q, want %q", name, got, wantPasswd)
			}
		})
	}
}
