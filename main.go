// Copyright (c) 2026 Nawakarit
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License v3.0.
package main

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"
)

// ─────────────────────────────────────────────
//  Types
// ─────────────────────────────────────────────

type Job struct {
	index int
	path  string
}

type Img struct {
	index int
	img   image.Image
	err   error
}

type Encoded struct {
	index int
	buf   *bytes.Buffer
	w, h  float64
	err   error
}

type FolderEntry struct {
	mu       sync.RWMutex
	path     string
	name     string
	imgCount int
	statuses []string
	done     bool
	errMsg   string
}

func (fe *FolderEntry) UpdateStatus(index int, status string) {
	fe.mu.Lock()
	defer fe.mu.Unlock()
	if index >= 0 && index < len(fe.statuses) {
		fe.statuses[index] = status
	}
}

func (fe *FolderEntry) SetDone(done bool) {
	fe.mu.Lock()
	defer fe.mu.Unlock()
	fe.done = done
}

func (fe *FolderEntry) SetErrMsg(msg string) {
	fe.mu.Lock()
	defer fe.mu.Unlock()
	fe.errMsg = msg
}

// encodeJPEG เข้ารหัสภาพเป็น JPEG quality 95 ลงใน buffer เพื่อประสิทธิภาพและขนาดไฟล์ที่ดีขึ้น
func encodeJPEG(buf *bytes.Buffer, img image.Image) error {
	return jpeg.Encode(buf, img, &jpeg.Options{Quality: 95})
}

// ─────────────────────────────────────────────
//  Globals
// ─────────────────────────────────────────────

var jpegPool = sync.Pool{
	New: func() any { return new(bytes.Buffer) },
}

var imageExts = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true,
	".gif": true, ".webp": true, ".bmp": true,
	".tiff": true, ".tif": true,
}

func isImage(name string) bool {
	return imageExts[strings.ToLower(filepath.Ext(name))]
}

// ─────────────────────────────────────────────
//  Helpers
// ─────────────────────────────────────────────

func collectImages(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && isImage(e.Name()) {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(out)
	return out, nil
}

func openFolder(path string) {
	switch runtime.GOOS {
	case "linux":
		exec.Command("xdg-open", path).Start()
	case "darwin":
		exec.Command("open", path).Start()
	case "windows":
		exec.Command("explorer", path).Start()
	}
}

// โหลด icon
func loadIcon(size int) fyne.Resource {
	var file string

	switch {
	case size >= 512:
		file = "icons/icon-512.png" ///ที่อยู่
	case size >= 256:
		file = "icons/icon-256.png"
	case size >= 128:
		file = "icons/icon-128.png"
	default:
		file = "icons/icon-64.png"
	}

	data, _ := iconFS.ReadFile(file)
	return fyne.NewStaticResource(file, data)
}

//go:embed icons/*
var iconFS embed.FS

//go:embed assets/font/Itim-Regular.ttf
var fontItim []byte
var myFont = fyne.NewStaticResource("Itim-Regular.ttf", fontItim)

var overlayW = color.NRGBA{250, 0, 0, 80}
var overlayB = color.NRGBA{0, 0, 0, 80}

//go:embed assets/lang/English.json
var enJSON []byte

//go:embed assets/lang/THAI.json
var thJSON []byte

// ฟังก์ชันสำหรับอัปเดตข้อความแสดงความเร็ว CPU//////////////////////////////////////////////////////////////////////////////////////
func main() {

	a := app.NewWithID("com.nawakarit.pdf_nwk")
	icon := loadIcon(64) //เอา data มาใช้
	w := a.NewWindow("pdf_nwk")
	w.SetIcon(icon)
	a.Settings().SetTheme(&MyTheme{})

	w.Resize(fyne.NewSize(660, 720))

	// ============================================================================
	// เปลี่ยนภาษา
	// ============================================================================
	var en map[string]string
	var th map[string]string
	json.Unmarshal(enJSON, &en)
	json.Unmarshal(thJSON, &th)

	// create i18n
	tr := NewI18n(en, th)

	// 🔥 language select // เปลี่ยนภาษา
	langSelect := NewSelect(tr, []string{"en", "th"}, func(val string) {
		tr.SetLang(val)
	})
	langSelect.SetSelected("en")

	//สถานะ
	var mu sync.Mutex
	var folders []*FolderEntry
	outputDir := ""
	converting := false

	outLabel := NewLabel(tr, "No save location yet")
	outLabel.Truncation = fyne.TextTruncateEllipsis
	outLabel.Alignment = fyne.TextAlignCenter

	statusLabel := NewLabel(tr, "Ready")
	statusLabel.Alignment = fyne.TextAlignCenter

	statusLabel.Wrapping = fyne.TextWrapWord
	globalProgress := widget.NewProgressBar()

	// ── Folder list ──
	folderList := widget.NewList(
		func() int {
			mu.Lock()
			defer mu.Unlock()
			return len(folders)
		},
		func() fyne.CanvasObject {
			nameLabel := widget.NewLabel("")
			nameLabel.TextStyle = fyne.TextStyle{Bold: true}
			stLabel := widget.NewLabel("")
			return container.NewBorder(nil, nil,
				widget.NewIcon(theme.FolderIcon()), stLabel,
				nameLabel,
			)
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			mu.Lock()
			defer mu.Unlock()
			if int(id) >= len(folders) {
				return
			}
			e := folders[id]
			c := obj.(*fyne.Container)
			c.Objects[0].(*widget.Label).SetText(
				fmt.Sprintf("%s  [%d image]", e.name, e.imgCount),
			)
			var st string
			e.mu.RLock()
			errMsg := e.errMsg
			done := e.done
			statusesCopy := make([]string, len(e.statuses))
			copy(statusesCopy, e.statuses)
			imgCount := e.imgCount
			e.mu.RUnlock()

			switch {
			case errMsg != "":
				st = "❌ " + errMsg
			case done:
				st = "✅ Done"
			default:
				n := 0
				for _, s := range statusesCopy {
					if strings.HasPrefix(s, "✅") {
						n++
					}
				}
				if n > 0 {
					st = fmt.Sprintf("🔄 %d/%d", n, imgCount)
				} else {
					st = "⏳ Wait"
				}
			}
			c.Objects[2].(*widget.Label).SetText(st)
		},
	)

	refreshList := func() { fyne.Do(func() { folderList.Refresh() }) }

	addFolderPaths := func(paths []string) {
		mu.Lock()
		defer mu.Unlock()
		seen := map[string]bool{}
		for _, fe := range folders {
			seen[fe.path] = true
		}
		for _, p := range paths {
			if seen[p] {
				continue
			}
			info, err := os.Stat(p)
			if err != nil || !info.IsDir() {
				continue
			}
			imgs, err := collectImages(p)
			if err != nil || len(imgs) == 0 {
				continue
			}
			st := make([]string, len(imgs))
			for i := range st {
				st[i] = "⏳ Wait"
			}
			folders = append(folders, &FolderEntry{
				path: p, name: filepath.Base(p),
				imgCount: len(imgs), statuses: st,
			})
			seen[p] = true
		}
		refreshList()
	}

	// Drag & Drop
	w.SetOnDropped(func(_ fyne.Position, uris []fyne.URI) {
		var paths []string
		for _, u := range uris {
			paths = append(paths, u.Path())
		}
		addFolderPaths(paths)
	})

	dropHint := NewLabel(tr, "Drag and drop")
	dropHint.Alignment = fyne.TextAlignCenter

	addBtn := NewButtonWithIcon(tr, "Add Folder", theme.FolderOpenIcon(), func() {
		dialog.ShowFolderOpen(func(u fyne.ListableURI, err error) {
			if err == nil && u != nil {
				addFolderPaths([]string{u.Path()})
			}

		}, w)

	})
	selectedID := -1
	folderList.OnSelected = func(id widget.ListItemID) { selectedID = int(id) }

	removeBtn := NewButtonWithIcon(tr, "Delete Selected", theme.DeleteIcon(), func() {
		mu.Lock()
		defer mu.Unlock()
		if selectedID >= 0 && selectedID < len(folders) {
			folders = append(folders[:selectedID], folders[selectedID+1:]...)
			selectedID = -1
			refreshList()
		}
	})

	clearBtn := NewButtonWithIcon(tr, "Clear All", theme.CancelIcon(), func() {
		mu.Lock()
		folders = nil
		mu.Unlock()
		refreshList()
	})

	chooseOutBtn := NewButtonWithIcon(tr, "Choose where to save the PDF", theme.FolderIcon(), func() {
		dialog.ShowFolderOpen(func(u fyne.ListableURI, err error) {
			if err == nil && u != nil {
				outputDir = u.Path()
				outLabel.SetText("📁 " + outputDir)
			}
		}, w)

	})

	var cancelFunc context.CancelFunc

	convertBtn := NewButtonWithIcon(tr, "Convert all to PDF", theme.MediaPlayIcon(), nil)
	convertBtn.Importance = widget.HighImportance

	resetConvertBtn := func() {
		convertBtn.SetText(tr.T("Convert all to PDF"))
		convertBtn.SetIcon(theme.MediaPlayIcon())
		convertBtn.Enable()
	}

	startConversion := func(snapshot []*FolderEntry, outDir string) {
		mu.Lock()
		converting = true
		mu.Unlock()

		ctx, cancel := context.WithCancel(context.Background())
		cancelFunc = cancel

		fyne.Do(func() {
			convertBtn.SetText("❌ Cancel")
			convertBtn.SetIcon(theme.CancelIcon())
			convertBtn.Enable()
			globalProgress.SetValue(0)
		})

		go func() {
			cores := runtime.NumCPU()
			totalFolders := len(snapshot)
			var errorMessages []string
			hasError := false
			canceled := false

			for fi, fe := range snapshot {
				if ctx.Err() != nil {
					canceled = true
					break
				}

				fe.mu.Lock()
				for i := range fe.statuses {
					fe.statuses[i] = "⏳ Wait"
				}
				fe.done = false
				fe.errMsg = ""
				fe.mu.Unlock()
				refreshList()

				imgs, err := collectImages(fe.path)
				if err != nil || len(imgs) == 0 {
					fe.SetErrMsg("images not found")
					refreshList()
					hasError = true
					errorMessages = append(errorMessages, fmt.Sprintf("%s: images not found", fe.name))
					continue
				}

				st := make([]string, len(imgs))
				for i := range st {
					st[i] = "⏳ Wait"
				}
				fe.mu.Lock()
				fe.statuses = st
				fe.imgCount = len(imgs)
				fe.mu.Unlock()

				outPath := filepath.Join(outDir, fe.name+".pdf")

				fyne.Do(func() {
					statusLabel.SetText(fmt.Sprintf(
						"[%d/%d] 📂 %s  –  %d image",
						fi+1, totalFolders, fe.name, len(imgs),
					))
					globalProgress.SetValue(float64(fi) / float64(totalFolders))
				})

				err = startPipeline(ctx, imgs, fe, globalProgress, statusLabel, cores, outPath, folderList)
				if err != nil {
					if errors.Is(err, context.Canceled) || ctx.Err() != nil {
						canceled = true
						fe.SetErrMsg("Canceled")
						refreshList()
						break
					}
					fe.SetErrMsg(err.Error())
					refreshList()
					hasError = true
					errorMessages = append(errorMessages, fmt.Sprintf("%s: %v", fe.name, err))
					continue
				}

				fe.SetDone(true)
				refreshList()
			}

			mu.Lock()
			converting = false
			mu.Unlock()

			fyne.Do(func() {
				resetConvertBtn()

				if canceled {
					statusLabel.SetText("❌ Conversion canceled")
					return
				}

				globalProgress.SetValue(1)

				if hasError {
					dialog.ShowError(fmt.Errorf("Some folders failed to convert:\n%s", strings.Join(errorMessages, "\n")), w)
				} else {
					dialog.ShowConfirm(
						"✅ Finish!",
						fmt.Sprintf("Convert %d folder finished 🎉\n\nopen folder ?", totalFolders),
						func(open bool) {
							if open {
								openFolder(outDir)
							}
						}, w,
					)
				}
			})
		}()
	}

	convertBtn.OnTapped = func() {
		mu.Lock()
		if converting {
			mu.Unlock()
			if cancelFunc != nil {
				cancelFunc()
				statusLabel.SetText("❌ Canceling...")
			}
			return
		}
		if len(folders) == 0 {
			mu.Unlock()
			dialog.ShowInformation("warn", "Please add the folder first.", w)
			return
		}
		if outputDir == "" {
			home, _ := os.UserHomeDir()
			desktop := filepath.Join(home, "Desktop")
			if _, err := os.Stat(desktop); err == nil {
				outputDir = desktop
			} else {
				outputDir = home
			}
			outLabel.SetText("📁 " + outputDir)
		}
		snapshot := make([]*FolderEntry, len(folders))
		copy(snapshot, folders)
		outDir := outputDir
		mu.Unlock()

		// Check for existing PDF files to warn user before overwriting
		var existingFiles []string
		for _, fe := range snapshot {
			p := filepath.Join(outDir, fe.name+".pdf")
			if _, err := os.Stat(p); err == nil {
				existingFiles = append(existingFiles, fe.name+".pdf")
			}
		}

		if len(existingFiles) > 0 {
			msg := fmt.Sprintf("The following PDF file(s) already exist:\n- %s\n\nDo you want to overwrite them?", strings.Join(existingFiles, "\n- "))
			dialog.ShowConfirm("⚠️ Overwrite Warning", msg, func(confirm bool) {
				if confirm {
					startConversion(snapshot, outDir)
				}
			}, w)
		} else {
			startConversion(snapshot, outDir)
		}
	}

	abbtn := widget.NewButton("!", func() {
		ShowInformation(tr, w, "about_title", "about_msg")
	})

	// ============================================================================
	// จัดวาง UI
	// ============================================================================
	topBar := container.NewBorder(nil, nil, nil, nil,
		container.NewVBox(dropHint, outLabel),
	)

	listBox := container.NewBorder(topBar, nil, nil, nil,
		container.NewScroll(folderList),
	)

	bottomBar := container.NewVBox(
		widget.NewSeparator(),
		statusLabel,
		container.NewCenter(container.NewHBox(addBtn, removeBtn, clearBtn, chooseOutBtn,
			container.NewGridWrap(fyne.NewSize(49, 35), langSelect),
			container.NewGridWrap(fyne.NewSize(10, 35), abbtn))),
		globalProgress,
		convertBtn,
	)

	w.SetContent(container.NewBorder(nil, bottomBar, nil, nil, listBox))
	w.ShowAndRun()
}
