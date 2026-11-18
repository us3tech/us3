package site_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func skipWithoutHugo(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("site tests do not run under -short")
	}
	if _, err := exec.LookPath("hugo"); err != nil {
		t.Skip("hugo not installed")
	}
}

// buildSite copies site/ and spec/ into a temp directory (same relative
// layout as the repository, so mounts of ../spec resolve), adds data
// (path relative to site/ -> content), builds with hugo and returns the
// output directory. An empty releaseURL param disables any network fetch.
func buildSite(t *testing.T, data map[string]string, env ...string) string {
	t.Helper()
	skipWithoutHugo(t)
	tmp := t.TempDir()
	for _, dir := range []string{"site", "spec"} {
		src := filepath.Join("..", dir)
		dst := filepath.Join(tmp, dir)
		err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(src, p)
			if dir == "site" {
				switch strings.SplitN(filepath.ToSlash(rel), "/", 2)[0] {
				case "public", "resources", ".hugo_build.lock":
					if d.IsDir() {
						return fs.SkipDir
					}
					return nil
				}
			}
			target := filepath.Join(dst, rel)
			if d.IsDir() {
				return os.MkdirAll(target, 0o755)
			}
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			return os.WriteFile(target, b, 0o644)
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	for rel, content := range data {
		p := filepath.Join(tmp, "site", rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	public := filepath.Join(tmp, "public")
	cmd := exec.Command("hugo", "--source", filepath.Join(tmp, "site"),
		"--destination", public, "--environment", "test")
	cmd.Env = append(os.Environ(), "HUGO_PARAMS_RELEASEURL=")
	cmd.Env = append(cmd.Env, env...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("hugo failed: %v\n%s", err, out)
	}
	return public
}

func readPage(t *testing.T, public, path string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(public, filepath.FromSlash(path)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestPagesExistInBothLanguages(t *testing.T) {
	pub := buildSite(t, nil)
	en := readPage(t, pub, "index.html")
	de := readPage(t, pub, "de/index.html")
	if !strings.Contains(en, `<html lang="en"`) || !strings.Contains(de, `<html lang="de"`) {
		t.Fatal("lang attributes missing")
	}
	if !strings.Contains(de, ">Anschauen<") || !strings.Contains(en, ">Watch<") {
		t.Error("navigation not translated")
	}
}

func TestNoThirdPartyRequests(t *testing.T) {
	pub := buildSite(t, nil)
	banned := []string{
		"fonts.googleapis.com", "fonts.gstatic.com", "cdn.", "unpkg",
		`src="http`, `src=http`, "@import url(http", "@import\"http", "url(http",
	}
	checked := 0
	err := filepath.WalkDir(pub, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		ext := filepath.Ext(p)
		if ext != ".html" && ext != ".css" {
			return nil
		}
		checked++
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		s := string(b)
		for _, bad := range banned {
			if strings.Contains(s, bad) {
				t.Errorf("%s contains %q", p, bad)
			}
		}
		if ext == ".html" && strings.Contains(s, `rel=stylesheet`) && strings.Contains(s, `rel=stylesheet href=http`) {
			t.Errorf("%s links an external stylesheet", p)
		}
		if ext == ".html" && strings.Contains(s, `rel="stylesheet" href="http`) {
			t.Errorf("%s links an external stylesheet", p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked < 3 {
		t.Fatalf("only %d html/css files checked", checked)
	}
}

func TestLanguageSwitchFallsBackToHome(t *testing.T) {
	pub := buildSite(t, map[string]string{
		"content/changes/2099-01-01-only-en.md": "---\ntitle: Only English\nproduct: turbine\nversion: 9.9.9\ndate: 2099-01-01\n---\nx\n",
	})
	en := readPage(t, pub, "changes/2099-01-01-only-en/index.html")
	if !strings.Contains(en, `href="/de/"`) {
		t.Errorf("switch on an English-only page should point at /de/")
	}
	home := readPage(t, pub, "index.html")
	if !strings.Contains(home, `href="/de/"`) {
		t.Errorf("home switch should point at /de/")
	}
}

func TestFontsAreSelfHosted(t *testing.T) {
	pub := buildSite(t, nil)
	for _, f := range []string{"fonts/OFL.txt", "fonts/montserrat-latin-800-normal.woff2", "fonts/ibm-plex-mono-latin-ext-500-normal.woff2"} {
		if _, err := os.Stat(filepath.Join(pub, f)); err != nil {
			t.Errorf("missing %s", f)
		}
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.FromSlash(path))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestBrandAssets(t *testing.T) {
	for _, name := range []string{"us3-mark", "us3-wordmark", "us3-logo", "turbine",
		"turbine-wordmark-tm", "pmpwrk", "honeycomb"} {
		b := mustRead(t, "assets/brand/"+name+".svg")
		if err := xml.Unmarshal(b, new(struct{})); err != nil {
			t.Errorf("%s: %v", name, err)
		}
		if bytes.Contains(b, []byte("<text")) || bytes.Contains(b, []byte("<style")) {
			t.Errorf("%s: text or style element", name)
		}
	}
	if b := mustRead(t, "assets/brand/us3-logo.svg"); bytes.Contains(b, []byte(`id="Background"`)) || bytes.Contains(b, []byte("<rect")) {
		t.Error("logo still carries a background rect")
	}
	if b := mustRead(t, "assets/brand/us3-wordmark.svg"); !bytes.Contains(b, []byte(`viewBox="491 8 337 135"`)) {
		t.Error("wordmark viewBox")
	}
	if b := mustRead(t, "assets/brand/us3-mark.svg"); !bytes.Contains(b, []byte(`viewBox="0 0 400 556"`)) {
		t.Error("mark viewBox")
	}
	h := mustRead(t, "assets/brand/honeycomb.svg")
	for _, want := range []string{`viewBox="0 0 34.64 60"`, `stop-opacity="0.012"`,
		`stroke-opacity="0.03"`, `stroke-opacity="0.55"`} {
		if !bytes.Contains(h, []byte(want)) {
			t.Errorf("honeycomb: missing %s", want)
		}
	}
}

func TestHeadLinksIconsAndPattern(t *testing.T) {
	pub := buildSite(t, nil)
	home := readPage(t, pub, "index.html")
	for _, want := range []string{`/favicon.svg`, `/favicon-32.png`, `og:image`} {
		if !strings.Contains(home, want) {
			t.Errorf("head lacks %s", want)
		}
	}
	css, err := filepath.Glob(filepath.Join(pub, "css", "site*.css"))
	if err != nil || len(css) == 0 {
		t.Fatalf("no css: %v", err)
	}
	b := mustRead(t, css[0])
	for _, want := range []string{".pattern-soft", "69.28px 120px", "linear-gradient(180deg"} {
		if !bytes.Contains(b, []byte(want)) {
			t.Errorf("css lacks %s", want)
		}
	}
	if bytes.Contains(b, []byte("url(http")) {
		t.Error("css pulls a remote url")
	}
}

func TestStartPageCopy(t *testing.T) {
	pub := buildSite(t, nil)
	de := readPage(t, pub, "de/index.html")
	en := readPage(t, pub, "index.html")
	for _, s := range []string{
		"Videos im Usenet ablegen. Verschlüsselt. Abspielbar ab der ersten Sekunde.",
		"So funktioniert US3", "Gebaut, um zu bleiben",
		"Und falls die US3-Werkzeuge mal nicht zur Hand sind:",
		"Was US3 nicht ist", "us3.tech/spec/v1",
	} {
		if !strings.Contains(de, s) {
			t.Errorf("de/index.html lacks %q", s)
		}
	}
	for _, s := range []string{"Watch", "How US3 works", "Built to last"} {
		if !strings.Contains(en, s) {
			t.Errorf("index.html lacks %q", s)
		}
	}
	for name, page := range map[string]string{"en": en, "de": de} {
		for _, s := range []string{"Plex", "Infuse", "VLC", "Arrival", "bergwanderung", "7zz"} {
			if strings.Contains(page, s) {
				t.Errorf("%s start page contains %q", name, s)
			}
		}
		nav := page[strings.Index(page, "<nav"):strings.Index(page, "</nav>")]
		if strings.Contains(nav, "Quelltext") {
			t.Errorf("%s nav contains Quelltext", name)
		}
	}
}

func TestStartPageLinks(t *testing.T) {
	pub := buildSite(t, nil)
	want := map[string][]string{
		"de/index.html": {"/de/neu/", "/de/hilfe/", "/de/pmpwrk/", "/de/spec/v1/"},
		"index.html":    {"/changes/", "/help/", "/pmpwrk/", "/spec/v1/"},
	}
	for page, hrefs := range want {
		body := readPage(t, pub, page)
		for _, h := range hrefs {
			if !strings.Contains(body, `href="`+h+`"`) {
				t.Errorf("%s lacks href=%q", page, h)
			}
		}
	}
}

type relFile struct{ product, platform, arch, kind, name string }

func fixtureSHA(name string) string {
	sum := sha256.Sum256([]byte(name))
	return hex.EncodeToString(sum[:])
}

func releaseJSON(version string, files ...relFile) map[string]string {
	var parts []string
	for _, f := range files {
		parts = append(parts, `{"product":"`+f.product+`","platform":"`+f.platform+`","arch":"`+f.arch+
			`","kind":"`+f.kind+`","name":"`+f.name+`","size":1,"sha256":"`+fixtureSHA(f.name)+`"}`)
	}
	return map[string]string{"data/release.json": `{"version":"` + version + `","date":"2026-12-01","files":[` +
		strings.Join(parts, ",") + `]}`}
}

func panel(t *testing.T, page, product, platform string) string {
	t.Helper()
	open := `id="dl-` + product + `-` + platform + `"`
	i := strings.Index(page, open)
	if i < 0 {
		t.Fatalf("no panel %s", open)
	}
	j := strings.Index(page[i:], "</section>")
	if j < 0 {
		t.Fatalf("panel %s not closed", open)
	}
	return page[i : i+j]
}

func TestNoReleaseShowsPlaceholder(t *testing.T) {
	pub := buildSite(t, nil)
	de := readPage(t, pub, "de/index.html")
	if !strings.Contains(de, "Demnächst hier verfügbar") {
		t.Error("placeholder missing")
	}
	if strings.Contains(de, `class="dl-panel"`) || strings.Contains(de, "Version 0.") {
		t.Error("panels or version without a release")
	}
	if !strings.Contains(de, `href="https://github.com/us3tech/us3"`) {
		t.Error("placeholder does not link the repository")
	}
}

func TestZipsOnly(t *testing.T) {
	pub := buildSite(t, releaseJSON("0.1.0",
		relFile{"turbine", "windows", "amd64", "zip", "turbine-0.1.0-windows-amd64.zip"},
		relFile{"turbine", "windows", "arm64", "zip", "turbine-0.1.0-windows-arm64.zip"},
		relFile{"turbine", "macos", "arm64", "zip", "turbine-0.1.0-macos-arm64.zip"},
	))
	page := readPage(t, pub, "de/index.html")
	if !strings.Contains(page, "Version 0.1.0") {
		t.Error("version line missing")
	}
	if !strings.Contains(page, `data-platform="windows"`) || !strings.Contains(page, `data-platform="macos"`) ||
		strings.Contains(page, `data-platform="synology"`) {
		t.Error("platform buttons wrong")
	}
	win := panel(t, page, "turbine", "windows")
	for _, f := range []string{"turbine-0.1.0-windows-amd64.zip", "turbine-0.1.0-windows-arm64.zip"} {
		if !strings.Contains(win, `dl-card"`) || !strings.Contains(win, "/releases/download/v0.1.0/"+f) {
			t.Errorf("windows panel lacks %s", f)
		}
	}
	if strings.Contains(win, "ZIP x64") || strings.Contains(win, "dl-secondary") {
		t.Error("secondary ZIP row although ZIPs are primary")
	}
	mac := panel(t, page, "turbine", "macos")
	if strings.Count(mac, `class="dl-card"`) != 1 || strings.Contains(mac, "brew install") {
		t.Error("macos panel should be one card without brew")
	}
	if !strings.Contains(win, `/de/hilfe/#windows`) {
		t.Error("help link missing")
	}
}

func TestFullSet(t *testing.T) {
	pub := buildSite(t, releaseJSON("0.1.0",
		relFile{"turbine", "windows", "amd64", "msi", "turbine-0.1.0-x64.msi"},
		relFile{"turbine", "windows", "arm64", "msi", "turbine-0.1.0-arm64.msi"},
		relFile{"turbine", "windows", "amd64", "zip", "turbine-0.1.0-windows-amd64.zip"},
		relFile{"turbine", "windows", "arm64", "zip", "turbine-0.1.0-windows-arm64.zip"},
		relFile{"turbine", "macos", "arm64", "dmg", "turbine-0.1.0.dmg"},
		relFile{"turbine", "synology", "amd64", "spk", "turbine-0.1.0-x86_64.spk"},
		relFile{"turbine", "synology", "arm64", "spk", "turbine-0.1.0-armv8.spk"},
	))
	page := readPage(t, pub, "index.html")
	win := panel(t, page, "turbine", "windows")
	i := strings.Index(win, `class="dl-secondary"`)
	if i < 0 {
		t.Fatal("no secondary row")
	}
	primary, secondary := win[:i], win[i:]
	for _, f := range []string{"x64.msi", "arm64.msi"} {
		if !strings.Contains(primary, f) || strings.Contains(secondary, f) {
			t.Errorf("%s not primary", f)
		}
	}
	for _, f := range []string{"windows-amd64.zip", "windows-arm64.zip"} {
		if !strings.Contains(secondary, f) || strings.Contains(primary, f) {
			t.Errorf("%s not secondary", f)
		}
	}
	mac := panel(t, page, "turbine", "macos")
	if !strings.Contains(mac, "turbine-0.1.0.dmg") || !strings.Contains(mac, "brew install --cask us3tech/tap/turbine") {
		t.Error("macos dmg or brew line missing")
	}
	syn := panel(t, page, "turbine", "synology")
	for _, s := range []string{"x86_64.spk", "armv8.spk", "Intel / AMD", "ARM 64 Bit", "DSM 7.0"} {
		if !strings.Contains(syn, s) {
			t.Errorf("synology panel lacks %q", s)
		}
	}
}

func TestUnknownAndIntelFilesAreSkipped(t *testing.T) {
	pub := buildSite(t, releaseJSON("0.1.0",
		relFile{"turbine", "macos", "amd64", "zip", "turbine-0.1.0-macos-amd64.zip"},
		relFile{"turbine", "windows", "386", "zip", "turbine-0.1.0-windows-386.zip"},
		relFile{"turbine", "windows", "amd64", "pkg", "turbine-0.1.0.pkg"},
	))
	page := readPage(t, pub, "de/index.html")
	for _, f := range []string{"macos-amd64", "windows-386", "turbine-0.1.0.pkg"} {
		if strings.Contains(page, f) {
			t.Errorf("page links %s", f)
		}
	}
	if !strings.Contains(page, "Demnächst hier verfügbar") {
		t.Error("placeholder missing")
	}
}

func TestPmpwrkPanelsOnlyListPmpwrk(t *testing.T) {
	pub := buildSite(t, releaseJSON("0.1.0",
		relFile{"turbine", "windows", "amd64", "zip", "turbine-0.1.0-windows-amd64.zip"},
		relFile{"pmpwrk", "windows", "amd64", "zip", "pmpwrk-0.1.0-windows-amd64.zip"},
	))
	page := readPage(t, pub, "index.html")
	if !strings.Contains(panel(t, page, "turbine", "windows"), "turbine-0.1.0-windows-amd64.zip") {
		t.Error("turbine panel lacks its file")
	}
	if strings.Contains(page, "pmpwrk-0.1.0-windows-amd64.zip") {
		t.Error("start page links a pmpwrk file")
	}
}

func TestDownloadBlockHasNoHiddenOrScript(t *testing.T) {
	pub := buildSite(t, releaseJSON("0.1.0",
		relFile{"turbine", "windows", "amd64", "zip", "turbine-0.1.0-windows-amd64.zip"}))
	page := readPage(t, pub, "index.html")
	blk := page[strings.Index(page, `class="tile-downloads"`):]
	blk = blk[:strings.Index(blk, "</article>")]
	if regexp.MustCompile(`\shidden[\s>=]`).MatchString(blk) {
		t.Error("download block uses the hidden attribute")
	}
	for _, bad := range []string{"onclick", "<script"} {
		if strings.Contains(blk, bad) {
			t.Errorf("download block contains %q", bad)
		}
	}
	if !strings.Contains(blk, `aria-controls="dl-turbine-windows"`) || !strings.Contains(blk, `aria-expanded="false"`) {
		t.Error("button wiring missing")
	}
}

func TestMalformedEntriesAreSkipped(t *testing.T) {
	pub := buildSite(t, map[string]string{"data/release.json": `{"version":"0.1.0","date":"2026-12-01","files":["x",3,null,` +
		`{"product":"turbine","platform":"windows","arch":"amd64","kind":"zip","name":"turbine-0.1.0-windows-amd64.zip","size":1,"sha256":"0"}]}`})
	page := readPage(t, pub, "index.html")
	if !strings.Contains(panel(t, page, "turbine", "windows"), "turbine-0.1.0-windows-amd64.zip") {
		t.Error("valid file not rendered")
	}
}

func TestNonStringVersionIsNoRelease(t *testing.T) {
	pub := buildSite(t, map[string]string{"data/release.json": `{"version":0.1,"date":"2026-12-01","files":[` +
		`{"product":"turbine","platform":"windows","arch":"amd64","kind":"zip","name":"a.zip","size":1,"sha256":"0"}]}`})
	page := readPage(t, pub, "index.html")
	if !strings.Contains(page, "Available here soon") || strings.Contains(page, "%!") || strings.Contains(page, "a.zip") {
		t.Error("non-string version should give the placeholder")
	}
}

func TestScriptIsExternalAndSingle(t *testing.T) {
	pub := buildSite(t, releaseJSON("0.1.0",
		relFile{"turbine", "windows", "amd64", "zip", "turbine-0.1.0-windows-amd64.zip"},
		relFile{"turbine", "macos", "arm64", "dmg", "turbine-0.1.0-macos-arm64.dmg"}))
	// JSON-LD is data, not code, and is not counted.
	script := regexp.MustCompile(`<script(>|\s[^>]*>)`)
	ldJSON := regexp.MustCompile(`<script type="?application/ld\+json"?>`)
	module := regexp.MustCompile(`<script type="?module"? src="?/js/site\.min\.[0-9a-f]+\.js"?[^>]*integrity="?sha256-`)
	n := 0
	err := filepath.WalkDir(pub, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".html") {
			return err
		}
		page := readPage(t, pub, strings.TrimPrefix(p, pub))
		if strings.Contains(page, `http-equiv="refresh"`) {
			return nil
		}
		n++
		if got := len(script.FindAllString(page, -1)) - len(ldJSON.FindAllString(page, -1)); got != 1 {
			t.Errorf("%s has %d script tags, want 1", p, got)
		} else if !module.MatchString(page) {
			t.Errorf("%s: script is not an external fingerprinted module", p)
		}
		if strings.Contains(page, "onclick=") {
			t.Errorf("%s uses onclick", p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("no pages checked")
	}
	home := readPage(t, pub, "index.html")
	blk := home[strings.Index(home, `class="tile-downloads"`):]
	blk = blk[:strings.Index(blk, "</article>")]
	if regexp.MustCompile(`\shidden[\s>=]`).MatchString(blk) {
		t.Error("download block uses the hidden attribute")
	}
	if !strings.Contains(home, `data-done="Copied"`) {
		t.Error("copy button lacks data-done")
	}
}

func TestSpecIsTheRepositoryText(t *testing.T) {
	pub := buildSite(t, nil)
	for _, c := range []struct{ md, page string }{
		{"../spec/US3-SPEC.md", "de/spec/v1/index.md"},
		{"../spec/US3-SPEC.en.md", "spec/v1/index.md"},
	} {
		if got, want := readPage(t, pub, c.page), string(mustRead(t, c.md)); got != want {
			t.Errorf("%s differs from %s (%d vs %d bytes)", c.page, c.md, len(got), len(want))
		}
	}
	for _, page := range []string{"spec/v1/index.html", "de/spec/v1/index.html"} {
		if strings.Contains(readPage(t, pub, page), `class="spec-commit"`) {
			t.Errorf("%s shows a commit although none was given", page)
		}
	}
}

func TestSpecAnchors(t *testing.T) {
	pub := buildSite(t, nil)
	numbered := regexp.MustCompile(`(?m)^#{2,3} \d`)
	for _, c := range []struct{ md, page string }{
		{"../spec/US3-SPEC.md", "de/spec/v1/index.html"},
		{"../spec/US3-SPEC.en.md", "spec/v1/index.html"},
	} {
		page := readPage(t, pub, c.page)
		for _, id := range []string{`id="s1"`, `id="s7.1"`, `id="s11"`, `id="s11.4"`} {
			if !strings.Contains(page, id) {
				t.Errorf("%s lacks %s", c.page, id)
			}
		}
		if !strings.Contains(page, `<a class="sec" href="#s7.1">§7.1</a>`) {
			t.Errorf("%s lacks the § link of 7.1", c.page)
		}
		want := len(numbered.FindAll(mustRead(t, c.md), -1))
		if got := strings.Count(page, `id="s`); got != want {
			t.Errorf("%s has %d section ids, want %d", c.page, got, want)
		}
		if got := strings.Count(page, "<h1"); got != 1 {
			t.Errorf("%s has %d h1 elements, want 1", c.page, got)
		}
	}
}

func TestSpecDropsNothing(t *testing.T) {
	pub := buildSite(t, nil)
	for _, page := range []string{"spec/v1/index.html", "de/spec/v1/index.html"} {
		if strings.Contains(readPage(t, pub, page), "raw HTML omitted") {
			t.Errorf("%s omits raw HTML", page)
		}
	}
	de := readPage(t, pub, "de/spec/v1/index.html")
	for _, s := range []string{"https://nzblnk.info/", "tenerflxp2jcdw3sst47ehsm@qfp3gwjv.net"} {
		if !strings.Contains(de, s) {
			t.Errorf("de spec lacks %q", s)
		}
	}
}

func TestUnnumberedHeadingKeepsPlainAnchor(t *testing.T) {
	pub := buildSite(t, map[string]string{"content/x.md": "---\ntitle: X\n---\n## Anhang\n\nText.\n"})
	page := readPage(t, pub, "x/index.html")
	if !strings.Contains(page, `id="anhang"`) || strings.Contains(page, `id="s`) || strings.Contains(page, `class="sec"`) {
		t.Error("unnumbered heading should keep the plain anchor")
	}
}

func TestSpecShowsStandAndCommit(t *testing.T) {
	pub := buildSite(t, nil, "HUGO_PARAMS_SPECCOMMIT_DE=abc1234", "HUGO_PARAMS_SPECCOMMIT_EN=def5678")
	for _, c := range []struct{ page, stand, commit, file string }{
		{"de/spec/v1/index.html", "Stand 2026-09-06", "abc1234", "US3-SPEC.md"},
		{"spec/v1/index.html", "As of 2026-09-06", "def5678", "US3-SPEC.en.md"},
	} {
		page := readPage(t, pub, c.page)
		if !strings.Contains(page, c.stand) {
			t.Errorf("%s lacks %q", c.page, c.stand)
		}
		link := regexp.MustCompile(`<a href="https://github\.com/us3tech/us3/commits/main/spec/` +
			regexp.QuoteMeta(c.file) + `"[^>]*>(?:<[^>]+>)*` + c.commit)
		if !link.MatchString(page) {
			t.Errorf("%s does not link %s to the history of %s", c.page, c.commit, c.file)
		}
	}
}

func TestSpecPageChrome(t *testing.T) {
	pub := buildSite(t, nil)
	de := readPage(t, pub, "de/spec/v1/index.html")
	en := readPage(t, pub, "spec/v1/index.html")
	for _, s := range []string{
		"Version 1, Entwurf", "Ein Token. Daraus folgt alles andere.",
		"Revision 2", "Als Markdown", "English version", "Änderungen im Repository",
		"Ableitung", "Abspielen", "Zustandslos", "Nicht verkettbar", "Rückwärtskompatibel",
		"Lizenziert unter CC BY 4.0.", `href="/de/spec/v1/index.md"`, `href="/spec/v1/"`,
		"<title>U/S3 — Usenet Simplified Streaming Specification (USSS / US3) · US3</title>",
	} {
		if !strings.Contains(de, s) {
			t.Errorf("de spec lacks %q", s)
		}
	}
	for _, s := range []string{
		"Version 1, Draft", "One token. Everything else follows from it.",
		"Licensed under CC BY 4.0.", `href="/spec/v1/index.md"`, `href="/de/spec/v1/"`,
		"Stateless", "Unlinkable", "Backwards compatible",
	} {
		if !strings.Contains(en, s) {
			t.Errorf("en spec lacks %q", s)
		}
	}
	for page, target := range map[string]string{"spec/index.html": "/spec/v1/", "de/spec/index.html": "/de/spec/v1/"} {
		body := readPage(t, pub, page)
		if !strings.Contains(body, `http-equiv="refresh"`) || !strings.Contains(body, target) {
			t.Errorf("%s does not redirect to %s", page, target)
		}
	}
}

var providerNames = []string{"Eweka", "Blocknews", "Newshosting", "Plex", "Infuse", "VLC"}

func TestPmpwrkPage(t *testing.T) {
	pub := buildSite(t, nil)
	de := readPage(t, pub, "de/pmpwrk/index.html")
	for _, s := range []string{
		"Ordner füllen, den Rest erledigt pmpwrk. Auch Monate später noch.",
		"Der Weg eines Videos", "Zweite Meinung", "Handbetrieb", "Nachsorge mit Ampel",
		"bergwanderung-4k.mp4", "Demnächst hier verfügbar",
	} {
		if !strings.Contains(de, s) {
			t.Errorf("de pmpwrk page lacks %q", s)
		}
	}
	en := readPage(t, pub, "pmpwrk/index.html")
	for _, s := range []string{
		"Fill a folder, pmpwrk does the rest. Even months later.", "The path of a video",
		"Second opinion", "manual mode", "Available here soon",
	} {
		if !strings.Contains(en, s) {
			t.Errorf("en pmpwrk page lacks %q", s)
		}
	}
	if !regexp.MustCompile(`<div class="care" aria-hidden="true">`).MatchString(de) {
		t.Error("aftercare list is not aria-hidden")
	}
	for _, page := range []string{de, en} {
		for _, name := range providerNames {
			if strings.Contains(page, name) {
				t.Errorf("pmpwrk page names %q", name)
			}
		}
	}
}

func TestTurbinePage(t *testing.T) {
	pub := buildSite(t, nil)
	de := readPage(t, pub, "de/turbine/index.html")
	for _, s := range []string{
		"Link anklicken, Film läuft.", "Vom Link zum Bild", "Im Mediaserver ansehen",
		"bergwanderung-4k.mp4", "Demnächst hier verfügbar", `href="/de/hilfe/"`,
	} {
		if !strings.Contains(de, s) {
			t.Errorf("de turbine page lacks %q", s)
		}
	}
	en := readPage(t, pub, "turbine/index.html")
	for _, s := range []string{
		"Click a link, the film plays.", "From link to picture", "Watch in your media server",
		"Available here soon",
	} {
		if !strings.Contains(en, s) {
			t.Errorf("en turbine page lacks %q", s)
		}
	}
	for _, page := range []string{de, en} {
		if !strings.Contains(page, `<div class="care" aria-hidden="true">`) {
			t.Error("library mockup is not aria-hidden")
		}
		for _, name := range providerNames {
			if strings.Contains(page, name) {
				t.Errorf("turbine page names %q", name)
			}
		}
	}
	for path, want := range map[string]string{"index.html": `href="/turbine/"`, "de/index.html": `href="/de/turbine/"`} {
		if !strings.Contains(readPage(t, pub, path), want) {
			t.Errorf("%s: navigation does not link %s", path, want)
		}
	}
	for path, want := range map[string]string{"index.html": `<a href="/turbine/">How turbine works</a>`, "de/index.html": `<a href="/de/turbine/">So arbeitet turbine</a>`} {
		if !strings.Contains(readPage(t, pub, path), want) {
			t.Errorf("%s: turbine tile does not link its page", path)
		}
	}
}

func TestNavigationMarksTheCurrentPage(t *testing.T) {
	pub := buildSite(t, nil)
	for path, want := range map[string]string{
		"de/turbine/index.html": "Anschauen", "pmpwrk/index.html": "Upload", "de/spec/v1/index.html": "Spezifikation",
	} {
		if got := regexp.MustCompile(`aria-current="page">([^<]*)`).FindAllStringSubmatch(readPage(t, pub, path), -1); len(got) != 1 || got[0][1] != want {
			t.Errorf("%s: current navigation entry is %v, want %q", path, got, want)
		}
	}
	for _, path := range []string{"index.html", "de/hilfe/index.html"} {
		if strings.Contains(readPage(t, pub, path), `aria-current="page"`) {
			t.Errorf("%s marks a navigation entry as current", path)
		}
	}
}

func TestMachineReadableDescriptions(t *testing.T) {
	pub := buildSite(t, releaseJSON("0.1.0",
		relFile{"turbine", "windows", "amd64", "zip", "turbine-0.1.0-windows-amd64.zip"}))
	ldRE := regexp.MustCompile(`<script type="application/ld\+json">(.*?)</script>`)
	ld := func(path string) map[string]any {
		t.Helper()
		m := ldRE.FindAllStringSubmatch(readPage(t, pub, path), -1)
		if len(m) != 1 {
			t.Fatalf("%s: %d JSON-LD blocks, want 1", path, len(m))
		}
		var v map[string]any
		if err := json.Unmarshal([]byte(m[0][1]), &v); err != nil {
			t.Fatalf("%s: JSON-LD does not parse: %v", path, err)
		}
		return v
	}
	if v := ld("index.html"); v["@type"] != "WebSite" || v["url"] != "https://us3.tech/" {
		t.Errorf("home JSON-LD: %v", v)
	}
	for path, name := range map[string]string{"turbine/index.html": "turbine", "de/pmpwrk/index.html": "pmpwrk"} {
		v := ld(path)
		if v["@type"] != "SoftwareApplication" || v["name"] != name || v["softwareVersion"] != "0.1.0" {
			t.Errorf("%s JSON-LD: %v", path, v)
		}
	}
	if strings.Contains(readPage(t, pub, "help/index.html"), "application/ld+json") {
		t.Error("help page carries JSON-LD")
	}
	if !strings.Contains(readPage(t, pub, "de/spec/v1/index.html"), `<link rel="alternate" type="text/markdown" href="/de/spec/v1/index.md">`) {
		t.Error("spec page does not point to its Markdown version")
	}
	llms := readPage(t, pub, "llms.txt")
	if !strings.HasPrefix(llms, "# US3\n\n> ") {
		t.Error("llms.txt does not start with a title and a summary")
	}
	for _, link := range regexp.MustCompile(`\]\(https://us3\.tech/([^)]*)\)`).FindAllStringSubmatch(llms, -1) {
		file := link[1]
		if file == "" || strings.HasSuffix(file, "/") {
			file += "index.html"
		}
		if _, err := os.Stat(filepath.Join(pub, file)); err != nil {
			t.Errorf("llms.txt links %s, which the build does not produce", link[1])
		}
	}
	for _, name := range providerNames {
		if strings.Contains(llms, name) {
			t.Errorf("llms.txt names %q", name)
		}
	}
}

func TestPmpwrkDownloadBlockLinksOnlyPmpwrk(t *testing.T) {
	pub := buildSite(t, releaseJSON("0.1.0",
		relFile{"turbine", "windows", "amd64", "zip", "turbine-0.1.0-windows-amd64.zip"},
		relFile{"pmpwrk", "windows", "amd64", "zip", "pmpwrk-0.1.0-windows-amd64.zip"},
		relFile{"turbine", "macos", "arm64", "zip", "turbine-0.1.0-macos-arm64.zip"},
		relFile{"pmpwrk", "macos", "arm64", "zip", "pmpwrk-0.1.0-macos-arm64.zip"},
	))
	for _, path := range []string{"pmpwrk/index.html", "de/pmpwrk/index.html"} {
		page := readPage(t, pub, path)
		if !strings.Contains(panel(t, page, "pmpwrk", "windows"), "pmpwrk-0.1.0-windows-amd64.zip") ||
			!strings.Contains(panel(t, page, "pmpwrk", "macos"), "pmpwrk-0.1.0-macos-arm64.zip") {
			t.Errorf("%s: pmpwrk panels lack their files", path)
		}
		if strings.Contains(page, "turbine-0.1.0") {
			t.Errorf("%s links a turbine file", path)
		}
	}
}

func helpPanel(t *testing.T, page, id string) string {
	t.Helper()
	i := strings.Index(page, `id="`+id+`"`)
	if i < 0 {
		t.Fatalf("no help panel %s", id)
	}
	rest := page[i+1:]
	j := len(rest)
	for _, end := range []string{`class="help-panel"`, "<aside"} {
		if k := strings.Index(rest, end); k >= 0 && k < j {
			j = k
		}
	}
	return rest[:j]
}

func TestHelpPage(t *testing.T) {
	pub := buildSite(t, nil)
	de := readPage(t, pub, "de/hilfe/index.html")
	en := readPage(t, pub, "help/index.html")
	for _, s := range []string{
		"Weitere Informationen", "Trotzdem ausführen", "Dennoch öffnen", "Manuelle Installation",
		"Ordner im Mediaserver hinzufügen", "turbine einrichten", "dem freigegebenen Ordner, den turbine anlegt",
		`href="https://github.com/us3tech/us3"`,
	} {
		if !strings.Contains(de, s) {
			t.Errorf("de help lacks %q", s)
		}
	}
	for _, s := range []string{"More info", "Run anyway", "Open Anyway", "Manual Install",
		"Add the folder in your media server", "the shared folder turbine creates"} {
		if !strings.Contains(en, s) {
			t.Errorf("en help lacks %q", s)
		}
	}
	for name, page := range map[string]string{"de": de, "en": en} {
		for _, id := range []string{"windows", "macos", "synology"} {
			if !strings.Contains(page, `role="tabpanel" id="`+id+`"`) || !strings.Contains(page, `aria-controls="`+id+`"`) {
				t.Errorf("%s help: tab or panel %s missing", name, id)
			}
		}
		if !strings.Contains(page, `role="tablist"`) || strings.Count(page, `role="tab"`) != 3 {
			t.Errorf("%s help: tablist wiring wrong", name)
		}
		if regexp.MustCompile(`\shidden[\s>=]`).MatchString(page) {
			t.Errorf("%s help hides content without JS", name)
		}
		if strings.Contains(page, "brew install") {
			t.Errorf("%s help offers brew without a macOS dmg", name)
		}
		for _, bad := range append([]string{"[ORDNER]", "[GITHUB]", "[org]", "[LIZENZ]", "shasum -a 256", "Get-FileHash"}, providerNames...) {
			if strings.Contains(page, bad) {
				t.Errorf("%s help contains %q without a release", name, bad)
			}
		}
	}
	if !strings.Contains(helpPanel(t, de, "synology"), "demnächst") ||
		!strings.Contains(helpPanel(t, en, "synology"), "coming soon") {
		t.Error("synology tab does not say coming soon")
	}
}

func TestHelpPageBrewLineNeedsDmg(t *testing.T) {
	pub := buildSite(t, releaseJSON("0.1.0", relFile{"turbine", "macos", "arm64", "dmg", "turbine-0.1.0-macos-arm64.dmg"}))
	for _, path := range []string{"de/hilfe/index.html", "help/index.html"} {
		if page := readPage(t, pub, path); !strings.Contains(page, "brew install --cask us3tech/tap/turbine") {
			t.Errorf("%s lacks the brew line although a dmg exists", path)
		}
	}
	pub = buildSite(t, releaseJSON("0.1.0", relFile{"turbine", "macos", "arm64", "zip", "turbine-0.1.0-macos-arm64.zip"}))
	if page := readPage(t, pub, "help/index.html"); strings.Contains(page, "brew install") {
		t.Error("brew line shown for a release without a dmg")
	}
}

func TestHelpPageChecksums(t *testing.T) {
	files := []relFile{
		{"turbine", "windows", "amd64", "msi", "turbine-0.1.0-windows-amd64.msi"},
		{"turbine", "windows", "amd64", "zip", "turbine-0.1.0-windows-amd64.zip"},
		{"turbine", "macos", "arm64", "dmg", "turbine-0.1.0-macos-arm64.dmg"},
		{"turbine", "synology", "amd64", "spk", "turbine-0.1.0-synology-amd64.spk"},
		{"pmpwrk", "windows", "amd64", "zip", "pmpwrk-0.1.0-windows-amd64.zip"},
	}
	pub := buildSite(t, releaseJSON("0.1.0", files...))
	for _, path := range []string{"de/hilfe/index.html", "help/index.html"} {
		page := readPage(t, pub, path)
		for _, c := range []struct{ panel, cmd string }{
			{"windows", "Get-FileHash -Algorithm SHA256 "}, {"macos", "shasum -a 256 "}, {"synology", "shasum -a 256 "},
		} {
			body := helpPanel(t, page, c.panel)
			for _, f := range files {
				if f.product != "turbine" || f.platform != c.panel {
					continue
				}
				if !strings.Contains(body, c.cmd+f.name) || !strings.Contains(body, fixtureSHA(f.name)) {
					t.Errorf("%s: %s lacks checksum line for %s", path, c.panel, f.name)
				}
			}
		}
		if strings.Contains(page, "pmpwrk-0.1.0") || strings.Contains(page, fixtureSHA("pmpwrk-0.1.0-windows-amd64.zip")) {
			t.Errorf("%s lists a pmpwrk file", path)
		}
		if strings.Contains(helpPanel(t, page, "synology"), "demnächst") || strings.Contains(helpPanel(t, page, "synology"), "coming soon") {
			t.Errorf("%s: synology says coming soon although the SPK exists", path)
		}
	}
}

func changeEntry(title, date, product, version, body string) string {
	return "---\ntitle: " + title + "\ndate: " + date + "\nproduct: " + product + "\nversion: " + version + "\n---\n" + body + "\n"
}

func TestChanges(t *testing.T) {
	pub := buildSite(t, map[string]string{
		"content/changes/2026-12-01-turbine-0.1.0.md":    changeEntry("First turbine", "2026-12-01", "turbine", "0.1.0", "- Alpha item"),
		"content/changes/2026-12-01-turbine-0.1.0.de.md": changeEntry("Erstes turbine", "2026-12-01", "turbine", "0.1.0", "- Alpha-Punkt"),
		"content/changes/2026-12-02-pmpwrk-0.1.0.md":     changeEntry("First pmpwrk", "2026-12-02", "pmpwrk", "0.1.0", "- Beta item"),
		"content/changes/2026-12-02-pmpwrk-0.1.0.de.md":  changeEntry("Erstes pmpwrk", "2026-12-02", "pmpwrk", "0.1.0", "- Beta-Punkt"),
		"content/changes/2026-12-03-only-en.md":          changeEntry("Only English", "2026-12-03", "spec", "v1 · Rev. 3", "x"),
		"data/upcoming.yaml":                             "- en: Coming thing\n  de: Kommendes Ding\n",
	})
	de := readPage(t, pub, "de/neu/index.html")
	if strings.Index(de, "Erstes pmpwrk") > strings.Index(de, "Erstes turbine") || !strings.Contains(de, "Erstes pmpwrk") {
		t.Errorf("de list should show newest first")
	}
	if !strings.Contains(de, "Kommendes Ding") || strings.Contains(de, "Coming thing") {
		t.Errorf("de list should show the German upcoming item")
	}
	if strings.Contains(de, "Only English") {
		t.Errorf("an entry without German text must not appear on the German list")
	}
	for _, f := range []string{"all", "turbine", "pmpwrk", "spec"} {
		if !strings.Contains(de, `data-filter="`+f+`"`) {
			t.Errorf("missing filter button %s", f)
		}
	}
	if strings.Contains(de, "hidden") {
		t.Errorf("without JS every entry must be visible (no hidden)")
	}
	if !strings.Contains(de, `data-product="turbine"`) || !strings.Contains(de, `type="application/rss+xml"`) {
		t.Errorf("entries need data-product and the head an RSS link")
	}
	en := readPage(t, pub, "changes/index.html")
	if !strings.Contains(en, "Only English") || !strings.Contains(en, "Coming thing") {
		t.Errorf("en list should show the English-only entry and upcoming")
	}
	for path, want := range map[string][]string{
		"changes/index.xml": {"First turbine", "Only English"},
		"de/neu/index.xml":  {"Erstes turbine", "Erstes pmpwrk"},
	} {
		raw := readPage(t, pub, path)
		if err := xml.Unmarshal([]byte(raw), new(struct{})); err != nil {
			t.Errorf("%s is not valid XML: %v", path, err)
		}
		for _, w := range want {
			if !strings.Contains(raw, w) {
				t.Errorf("%s lacks %q", path, w)
			}
		}
	}
	feed := readPage(t, pub, "de/neu/index.xml")
	for _, w := range []string{"<title>US3 – Was ist neu</title>", "<description>Alle Versionen von turbine und pmpwrk"} {
		if !strings.Contains(feed, w) {
			t.Errorf("German feed lacks %q", w)
		}
	}
	if strings.Contains(feed, "Only English") {
		t.Errorf("German feed must not contain the English-only entry")
	}
}

func TestChangesSlugsComeFromTheFileName(t *testing.T) {
	pub := buildSite(t, map[string]string{
		"content/changes/2026-12-01-a.md":    changeEntry("First A", "2026-12-01", "turbine", "0.1.0", "- a"),
		"content/changes/2026-12-01-a.de.md": changeEntry("Die erste Version", "2026-12-01", "turbine", "0.1.0", "- a"),
		"content/changes/2026-12-01-b.md":    changeEntry("First B", "2026-12-01", "pmpwrk", "0.1.0", "- b"),
		"content/changes/2026-12-01-b.de.md": changeEntry("Die erste Version", "2026-12-01", "pmpwrk", "0.1.0", "- b"),
	})
	list := readPage(t, pub, "de/neu/index.html")
	for _, slug := range []string{"2026-12-01-a", "2026-12-01-b"} {
		readPage(t, pub, "de/neu/"+slug+"/index.html")
		readPage(t, pub, "changes/"+slug+"/index.html")
		if !strings.Contains(list, `href="/de/neu/`+slug+`/"`) {
			t.Errorf("de list does not link %s", slug)
		}
	}
}

func TestNoHomeFeed(t *testing.T) {
	pub := buildSite(t, nil)
	for _, p := range []string{"index.xml", "de/index.xml"} {
		if _, err := os.Stat(filepath.Join(pub, p)); err == nil {
			t.Errorf("%s exists; only the changes feeds are published", p)
		}
	}
	for _, p := range []string{"changes/index.xml", "de/neu/index.xml"} {
		readPage(t, pub, p)
	}
	for _, p := range []string{"index.html", "de/index.html"} {
		if strings.Contains(readPage(t, pub, p), "application/rss+xml") {
			t.Errorf("%s links a feed", p)
		}
	}
}

func TestInternalLinksResolve(t *testing.T) {
	pub := buildSite(t, releaseJSON("0.1.0",
		relFile{"turbine", "windows", "amd64", "zip", "turbine-0.1.0-windows-amd64.zip"},
		relFile{"turbine", "windows", "arm64", "zip", "turbine-0.1.0-windows-arm64.zip"},
		relFile{"turbine", "macos", "arm64", "dmg", "turbine-0.1.0-macos-arm64.dmg"},
		relFile{"turbine", "synology", "amd64", "spk", "turbine-0.1.0-synology-amd64.spk"},
		relFile{"pmpwrk", "windows", "amd64", "zip", "pmpwrk-0.1.0-windows-amd64.zip"},
	))
	if !strings.Contains(readPage(t, pub, "index.html"), `class="dl-panel"`) {
		t.Fatal("release fixture did not render download panels")
	}
	ref := regexp.MustCompile(`(?:href|src)="(/[^"#?]*)`)
	checked := 0
	err := filepath.WalkDir(pub, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".html") {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		for _, m := range ref.FindAllStringSubmatch(string(b), -1) {
			target := m[1]
			if strings.HasPrefix(target, "//") {
				continue
			}
			full := filepath.Join(pub, filepath.FromSlash(target))
			if st, err := os.Stat(full); strings.HasSuffix(target, "/") || (err == nil && st.IsDir()) {
				full = filepath.Join(full, "index.html")
			}
			if st, err := os.Stat(full); err != nil || st.IsDir() {
				t.Errorf("%s links to %s, which does not exist", strings.TrimPrefix(p, pub), target)
			}
			checked++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked < 50 {
		t.Fatalf("only %d links checked", checked)
	}
}
