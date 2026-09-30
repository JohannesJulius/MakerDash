//go:build windows

package main

// Updates über GitHub Releases.
// Erwartete Release-Dateien: PicoDashboard-Setup.exe und SHA256SUMS.txt

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/windows"
)

// Werden beim Bauen gesetzt: -ldflags "-X main.AppVersion=1.2.3 -X main.UpdateRepo=user/repo"
var (
	AppVersion = "0.0.0-dev"
	UpdateRepo = "JohannesJulius/picodashboard"
)

const setupAsset = "PicoDashboard-Setup.exe"
const sumsAsset = "SHA256SUMS.txt"

var httpClient = &http.Client{Timeout: 60 * time.Second}

func httpGet(url string) (*http.Response, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "PicoDashboard/"+AppVersion)
	req.Header.Set("Accept", "application/vnd.github+json")
	return httpClient.Do(req)
}

// download lädt eine Datei mit Fortschritt (0–100) herunter.
func download(url, dst string, progress func(int)) error {
	resp, err := httpGet(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	tmp := dst + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	total := resp.ContentLength
	var done int64
	buf := make([]byte, 64*1024)
	last := -1
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, err := f.Write(buf[:n]); err != nil {
				f.Close()
				return err
			}
			done += int64(n)
			if total > 0 && progress != nil {
				if p := int(done * 100 / total); p != last {
					last = p
					progress(p)
				}
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			f.Close()
			return rerr
		}
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

// ---------------- Versionen ----------------

func parseVersion(s string) [3]int {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	if i := strings.IndexAny(s, "-+ "); i >= 0 {
		s = s[:i]
	}
	var v [3]int
	for i, p := range strings.SplitN(s, ".", 3) {
		v[i], _ = strconv.Atoi(p)
	}
	return v
}

func newerVersion(latest, current string) bool {
	a, b := parseVersion(latest), parseVersion(current)
	for i := 0; i < 3; i++ {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}

// ---------------- Updater ----------------

type UpdateState struct {
	State     string `json:"state"` // idle, checking, uptodate, available, downloading, verifying, installing, error, disabled
	Current   string `json:"current"`
	Latest    string `json:"latest"`
	Notes     string `json:"notes"`
	Progress  int    `json:"progress"`
	Error     string `json:"error"`
	LastCheck string `json:"lastCheck"`
	Repo      string `json:"repo"`
	URL       string `json:"url"`
}

type Updater struct {
	mu        sync.Mutex
	st        UpdateState
	setupURL  string
	sumsURL   string
	notified  string
}

var updater = &Updater{st: UpdateState{State: "idle", Current: AppVersion}}

func updateRepo() string {
	r := UpdateRepo
	withConfig(func(c *Config) {
		if c.UpdateRepo != "" {
			r = c.UpdateRepo
		}
	}, false)
	return strings.Trim(strings.TrimSpace(r), "/")
}

func (u *Updater) Snapshot() UpdateState {
	u.mu.Lock()
	defer u.mu.Unlock()
	s := u.st
	s.Current = AppVersion
	s.Repo = updateRepo()
	if s.Repo == "" {
		s.State = "disabled"
	}
	return s
}

func (u *Updater) set(fn func(s *UpdateState)) {
	u.mu.Lock()
	fn(&u.st)
	u.mu.Unlock()
	markDirty()
}

type ghRelease struct {
	TagName string `json:"tag_name"`
	Name    string `json:"name"`
	Body    string `json:"body"`
	HTMLURL string `json:"html_url"`
	Draft   bool   `json:"draft"`
	Assets  []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

// Check fragt die neueste Version ab. manual = vom Benutzer ausgelöst.
func (u *Updater) Check(manual bool) {
	repo := updateRepo()
	if repo == "" {
		return
	}
	u.mu.Lock()
	busy := u.st.State == "checking" || u.st.State == "downloading" || u.st.State == "installing"
	u.mu.Unlock()
	if busy {
		return
	}
	u.set(func(s *UpdateState) { s.State = "checking"; s.Error = "" })
	api := "https://api.github.com"
	if v := os.Getenv("PICODASH_UPDATE_API"); v != "" { // nur für Tests
		api = v
	}
	resp, err := httpGet(api + "/repos/" + repo + "/releases/latest")
	now := time.Now().Format("02.01.2006 15:04")
	if err != nil {
		u.set(func(s *UpdateState) { s.State = "error"; s.Error = "Keine Verbindung zum Update-Server"; s.LastCheck = now })
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		u.set(func(s *UpdateState) {
			s.State = "uptodate"
			s.LastCheck = now
			s.Error = ""
		})
		if manual {
			toast("info", "Noch keine Veröffentlichung gefunden – du bist auf dem neuesten Stand.")
		}
		return
	}
	if resp.StatusCode != 200 {
		u.set(func(s *UpdateState) { s.State = "error"; s.Error = fmt.Sprintf("Update-Server antwortet mit %d", resp.StatusCode); s.LastCheck = now })
		return
	}
	var rel ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		u.set(func(s *UpdateState) { s.State = "error"; s.Error = "Antwort nicht lesbar"; s.LastCheck = now })
		return
	}
	var setupURL, sumsURL string
	for _, a := range rel.Assets {
		switch a.Name {
		case setupAsset:
			setupURL = a.URL
		case sumsAsset:
			sumsURL = a.URL
		}
	}
	latest := strings.TrimPrefix(rel.TagName, "v")
	if !newerVersion(latest, AppVersion) || setupURL == "" {
		u.set(func(s *UpdateState) {
			s.State = "uptodate"
			s.Latest = latest
			s.LastCheck = now
		})
		if manual {
			toast("ok", "Pico Dashboard ist auf dem neuesten Stand.")
		}
		return
	}
	u.mu.Lock()
	u.setupURL, u.sumsURL = setupURL, sumsURL
	first := u.notified != latest
	u.notified = latest
	u.mu.Unlock()
	u.set(func(s *UpdateState) {
		s.State = "available"
		s.Latest = latest
		s.Notes = rel.Body
		s.URL = rel.HTMLURL
		s.LastCheck = now
	})
	if first && !uiVisible() {
		notifyUser("Update verfügbar", "Pico Dashboard "+latest+" ist verfügbar. Klicke hier zum Installieren.", "updates")
	}
}

// Install lädt den Installer, prüft die Prüfsumme und startet ihn.
func (u *Updater) Install() {
	u.mu.Lock()
	if u.st.State != "available" && u.st.State != "error" || u.setupURL == "" {
		u.mu.Unlock()
		return
	}
	setupURL, sumsURL, latest := u.setupURL, u.sumsURL, u.st.Latest
	u.mu.Unlock()

	dir := filepath.Join(os.TempDir(), "PicoDashboard-Update")
	os.MkdirAll(dir, 0o755)
	file := filepath.Join(dir, "PicoDashboard-Setup-"+latest+".exe")
	u.set(func(s *UpdateState) { s.State = "downloading"; s.Progress = 0; s.Error = "" })
	if err := download(setupURL, file, func(p int) { u.set(func(s *UpdateState) { s.Progress = p }) }); err != nil {
		u.set(func(s *UpdateState) { s.State = "error"; s.Error = "Download fehlgeschlagen: " + err.Error() })
		return
	}
	u.set(func(s *UpdateState) { s.State = "verifying" })
	if sumsURL == "" {
		u.set(func(s *UpdateState) { s.State = "error"; s.Error = "Prüfsumme fehlt im Release" })
		return
	}
	if err := verifySHA256(file, sumsURL); err != nil {
		os.Remove(file)
		u.set(func(s *UpdateState) { s.State = "error"; s.Error = err.Error() })
		return
	}
	u.set(func(s *UpdateState) { s.State = "installing" })
	log.Println("Starte Update", latest)
	// Der Installer fragt nach Administratorrechten, beendet die App,
	// installiert still und startet die neue Version.
	if err := windows.ShellExecute(0, u16("open"), u16(file), u16("/S /UPDATE"), u16(dir), 1); err != nil {
		msg := "Installer konnte nicht gestartet werden"
		if errors.Is(err, windows.ERROR_CANCELLED) {
			msg = "Update abgebrochen (Administratorrechte wurden nicht erteilt)"
		}
		u.set(func(s *UpdateState) { s.State = "available"; s.Error = msg })
		toast("error", msg)
		return
	}
}

func verifySHA256(file, sumsURL string) error {
	resp, err := httpGet(sumsURL)
	if err != nil {
		return errors.New("Prüfsumme konnte nicht geladen werden")
	}
	defer resp.Body.Close()
	want := ""
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == setupAsset {
			want = strings.ToLower(f[0])
		}
	}
	if want == "" {
		return errors.New("Prüfsumme für den Installer fehlt")
	}
	fh, err := os.Open(file)
	if err != nil {
		return err
	}
	defer fh.Close()
	h := sha256.New()
	io.Copy(h, fh)
	if hex.EncodeToString(h.Sum(nil)) != want {
		return errors.New("Prüfsumme stimmt nicht – Download beschädigt")
	}
	return nil
}

// loop prüft beim Start und danach alle 6 Stunden.
func (u *Updater) loop() {
	time.Sleep(15 * time.Second)
	for {
		auto := true
		withConfig(func(c *Config) { auto = !c.NoAutoUpdateCheck }, false)
		if auto {
			u.Check(false)
		}
		time.Sleep(6 * time.Hour)
	}
}
