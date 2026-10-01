// Copyright 2026 Syed Abdul Waheed
// SPDX-License-Identifier: Apache-2.0

package main

// Saving books to the Downloads folder. A browser downloads a book when its
// link is followed; the native window's web view does not, so there the
// page asks the program to save it instead.

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

// downloadsDir is the user's Downloads folder, or the home folder when
// there is none.
func downloadsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if d := filepath.Join(home, "Downloads"); isDir(d) {
		return d, nil
	}
	return home, nil
}

func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

// saveCopy copies src into dir as name, numbering the name if a file of
// that name is there already, and returns the path it wrote.
func saveCopy(src, dir, name string) (string, error) {
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for n := 1; ; n++ {
		p := filepath.Join(dir, name)
		if n > 1 {
			p = filepath.Join(dir, fmt.Sprintf("%s (%d)%s", stem, n, ext))
		}
		out, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if os.IsExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		in, err := os.Open(src)
		if err != nil {
			out.Close()
			os.Remove(p)
			return "", err
		}
		_, err = io.Copy(out, in)
		in.Close()
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			os.Remove(p)
			return "", err
		}
		return p, nil
	}
}

type savedView struct {
	Path string `json:"path"`
	Name string `json:"name"`
}

func (s *guiServer) saveJobs(ids []string) ([]savedView, error) {
	dir, err := downloadsDir()
	if err != nil {
		return nil, err
	}
	var out []savedView
	for _, id := range ids {
		j, ok := s.finished(id)
		if !ok {
			continue
		}
		p, err := saveCopy(j.epub, dir, epubName(j.view.File))
		if err != nil {
			return out, err
		}
		s.mu.Lock()
		s.saved = append(s.saved, p)
		s.mu.Unlock()
		out = append(out, savedView{p, filepath.Base(p)})
	}
	return out, nil
}

func (s *guiServer) save(w http.ResponseWriter, r *http.Request) {
	saved, err := s.saveJobs([]string{r.PathValue("id")})
	switch {
	case err != nil:
		http.Error(w, "saving: "+err.Error(), http.StatusInternalServerError)
	case len(saved) == 0:
		http.NotFound(w, r)
	default:
		writeJSON(w, http.StatusOK, saved[0])
	}
}

func (s *guiServer) saveAll(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	ids := append([]string(nil), s.order...)
	s.mu.Unlock()
	saved, err := s.saveJobs(ids)
	if err != nil {
		http.Error(w, "saving: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, saved)
}

// reveal shows a saved book in the file manager. Only files this program
// saved can be shown.
func (s *guiServer) reveal(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	ok := slices.Contains(s.saved, body.Path)
	s.mu.Unlock()
	if !ok {
		http.Error(w, "not a file this program saved", http.StatusForbidden)
		return
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("explorer", "/select,"+body.Path)
	case "darwin":
		cmd = exec.Command("open", "-R", body.Path)
	default:
		cmd = exec.Command("xdg-open", filepath.Dir(body.Path))
	}
	if err := cmd.Start(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	go cmd.Wait()
	w.WriteHeader(http.StatusNoContent)
}
