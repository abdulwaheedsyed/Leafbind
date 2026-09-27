package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSaveCopy(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.epub")
	os.WriteFile(src, []byte("book"), 0o644)
	out := filepath.Join(dir, "out")
	os.Mkdir(out, 0o755)
	var got []string
	for range 3 {
		p, err := saveCopy(src, out, "My Book.epub")
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, filepath.Base(p))
	}
	want := []string{"My Book.epub", "My Book (2).epub", "My Book (3).epub"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("save %d named %q, want %q", i, got[i], want[i])
		}
	}
	if b, _ := os.ReadFile(filepath.Join(out, "My Book (2).epub")); string(b) != "book" {
		t.Error("the copy differs")
	}
}

// Saving puts finished books in the Downloads folder; only those may then
// be shown in the file manager.
func TestGUISave(t *testing.T) {
	if testing.Short() {
		t.Skip("starts the PDF engine; skipped with -short")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	os.Mkdir(filepath.Join(home, "Downloads"), 0o755)

	s, base := startGUI(t, true)
	ch := events(t, s, base)
	<-ch
	id := upload(t, s, base, map[string][]byte{"Saved.pdf": makePDF([]testPage{{W: 400, H: 600}})})[0].ID
	waitJobs(t, ch, stateReady, id)
	b, _ := json.Marshal(convertRequest{IDs: []string{id}, Settings: defaultSettings()})
	tok := map[string]string{"X-Token": s.token}
	req(t, "POST", base+"/api/convert", bytes.NewReader(b), tok)
	waitJobs(t, ch, stateDone, id)

	var sv savedView
	res := req(t, "POST", base+"/api/jobs/"+id+"/save", nil, tok)
	if err := json.NewDecoder(res.Body).Decode(&sv); err != nil || sv.Name != "Saved.epub" || filepath.Dir(sv.Path) != filepath.Join(home, "Downloads") {
		t.Fatalf("saved as %+v, %v", sv, err)
	}
	if _, err := os.Stat(sv.Path); err != nil {
		t.Fatal("the book is not in Downloads")
	}
	var all []savedView
	json.NewDecoder(req(t, "POST", base+"/api/save-all", nil, tok).Body).Decode(&all)
	if len(all) != 1 || all[0].Name != "Saved (2).epub" {
		t.Errorf("save-all wrote %+v", all)
	}
	other, _ := json.Marshal(map[string]string{"path": "/etc/passwd"})
	if res := req(t, "POST", base+"/api/reveal", bytes.NewReader(other), tok); res.StatusCode != 403 {
		t.Errorf("revealing a file it did not save: status %d, want 403", res.StatusCode)
	}
	if res := req(t, "POST", base+"/api/jobs/"+id+"/save", nil, nil); res.StatusCode != 401 {
		t.Errorf("saving without the token: status %d", res.StatusCode)
	}
}
