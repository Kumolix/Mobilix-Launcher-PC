package main

import "testing"

func TestOfflineUUID(t *testing.T) {
	// известное значение для офлайн-игрока "Notch" в Minecraft
	if got := offlineUUID("Notch"); got != "b50ad385-829d-3141-a216-7e7d7539ba7f" {
		t.Fatalf("uuid %s", got)
	}
}

func TestMavenPath(t *testing.T) {
	p, ok := mavenPath("net.fabricmc:fabric-loader:0.16.9")
	if !ok || p != "net/fabricmc/fabric-loader/0.16.9/fabric-loader-0.16.9.jar" {
		t.Fatal(p)
	}
	p, _ = mavenPath("org.lwjgl:lwjgl:3.3.3:natives-windows")
	if p != "org/lwjgl/lwjgl/3.3.3/lwjgl-3.3.3-natives-windows.jar" {
		t.Fatal(p)
	}
}

func TestRules(t *testing.T) {
	win := []Rule{{Action: "allow"}, {Action: "disallow", OS: &OSRule{Name: "osx"}}}
	if !rulesOK(win, nil) {
		t.Fatal("allow all except osx")
	}
	only := []Rule{{Action: "allow", OS: &OSRule{Name: "osx"}}}
	if rulesOK(only, nil) {
		t.Fatal("osx only must be rejected")
	}
	demo := []Rule{{Action: "allow", Features: map[string]bool{"is_demo_user": true}}}
	if rulesOK(demo, map[string]bool{}) {
		t.Fatal("demo feature must be rejected")
	}
}

func TestCleanNick(t *testing.T) {
	if cleanNick("a b") != "Player" || cleanNick("Привет") != "Player" || cleanNick(" Steve_01 ") != "Steve_01" {
		t.Fatal("nick")
	}
}

func TestJavaOK(t *testing.T) {
	if javaOK(8, 17) || !javaOK(8, 8) || !javaOK(17, 21) || javaOK(21, 17) {
		t.Fatal("java compat")
	}
}

func TestPackHelpers(t *testing.T) {
	if l, v := loaderFromDeps(map[string]string{"minecraft": "1.20.1", "fabric-loader": "0.15.7"}); l != "fabric" || v != "0.15.7" {
		t.Fatal(l, v)
	}
	if l, _ := loaderFromDeps(map[string]string{"minecraft": "1.21"}); l != "vanilla" {
		t.Fatal(l)
	}
	if safeJoin("/a/b", "../x") != "" || safeJoin("/a/b", "mods/x.jar") == "" {
		t.Fatal("safeJoin")
	}
	if mcMinor("1.21.3") != 21 || mcMinor("1.8.9") != 8 {
		t.Fatal("mcMinor")
	}
}

func TestSkinUUIDWidth(t *testing.T) {
	for _, slim := range []bool{true, false} {
		u := skinUUID("Steve", slim)
		if len(u) != 36 {
			t.Fatal(u)
		}
	}
	if skinUUID("Steve", true) == skinUUID("Steve", false) {
		t.Fatal("same uuid for both widths")
	}
	if packFormat("1.21.3") < 34 || packFormat("1.8.9") != 1 {
		t.Fatal("packFormat")
	}
}

func TestPackFormatTable(t *testing.T) {
	for mc, want := range map[string]int{"1.8.9": 1, "1.12.2": 3, "1.16.5": 6, "1.19.2": 9, "1.19.4": 13, "1.20.1": 15, "1.20.4": 22, "1.21": 34, "1.21.1": 34, "1.21.4": 46, "1.21.11": 75} {
		if got := packFormat(mc); got != want {
			t.Errorf("%s: %d != %d", mc, got, want)
		}
	}
}
