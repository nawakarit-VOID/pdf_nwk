// Copyright (c) 2026 Nawakarit
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License v3.0.
package main

import (
	"bytes"
	"embed"
	_ "embed"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"

	_ "image/gif"
	"image/jpeg"
	_ "image/jpeg"
	_ "image/png"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"

	"os/exec"

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
}

type Encoded struct {
	index int
	buf   *bytes.Buffer
	w, h  float64
}

type FolderEntry struct {
	path     string
	name     string
	imgCount int
	statuses []string
	done     bool
	errMsg   string
}

// encodeJPEG เข้ารหัสภาพเป็น JPEG quality 100 ลงใน buffer
func encodeJPEG(buf *bytes.Buffer, img image.Image) {
	jpeg.Encode(buf, img, &jpeg.Options{Quality: 100}) //nolint:errcheck
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

func updateStatus(index int, status string, statuses []string) {
	if index >= 0 && index < len(statuses) {
		statuses[index] = status
	}
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
			switch {
			case e.errMsg != "":
				st = "❌ " + e.errMsg
			case e.done:
				st = "✅ Done"
			default:
				n := 0
				for _, s := range e.statuses {
					if strings.HasPrefix(s, "✅") {
						n++
					}
				}
				if n > 0 {
					st = fmt.Sprintf("🔄 %d/%d", n, e.imgCount)
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

	convertBtn := NewButtonWithIcon(tr, "Convert all to PDF", theme.MediaPlayIcon(), nil)
	convertBtn.Importance = widget.HighImportance

	convertBtn.OnTapped = func() {
		mu.Lock()
		if converting {
			mu.Unlock()
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
		converting = true
		mu.Unlock()

		convertBtn.Disable()
		globalProgress.SetValue(0)

		go func() {
			cores := runtime.NumCPU()
			totalFolders := len(snapshot)

			for fi, fe := range snapshot {
				for i := range fe.statuses {
					fe.statuses[i] = "⏳ Wait"
				}
				fe.done = false
				fe.errMsg = ""
				refreshList()

				imgs, err := collectImages(fe.path)
				if err != nil || len(imgs) == 0 {
					fe.errMsg = "images not found"
					refreshList()
					continue
				}
				fe.statuses = make([]string, len(imgs))
				for i := range fe.statuses {
					fe.statuses[i] = "⏳ Wait"
				}
				fe.imgCount = len(imgs)

				outPath := filepath.Join(outDir, fe.name+".pdf")

				fyne.Do(func() {
					statusLabel.SetText(fmt.Sprintf(
						"[%d/%d] 📂 %s  –  %d image",
						fi+1, totalFolders, fe.name, len(imgs),
					))
					globalProgress.SetValue(float64(fi) / float64(totalFolders))
				})

				startPipeline(imgs, fe.statuses, globalProgress, statusLabel, cores, outPath, folderList)

				fe.done = true
				refreshList()
			}

			mu.Lock()
			converting = false
			mu.Unlock()

			fyne.Do(func() {
				globalProgress.SetValue(1)
				convertBtn.Enable()
			})

			dialog.ShowConfirm(
				"✅ เสร็จสิ้น!",
				fmt.Sprintf("แปลง %d folder เสร็จแล้ว 🎉\n\nเปิด folder ที่บันทึก?", totalFolders),
				func(open bool) {
					if open {
						openFolder(outDir)
					}
				}, w,
			)
		}()
	}

	abbtn := widget.NewButton("!", func() {
		dialog.ShowInformation("about", "pdf_nwk v2.1.0\nGolang + fyne\n\nโปรแกรมแปลงรูปภาพเป็น pdf (เร็วมาก กินแรมน้อย)\nจริงๆสร้างมาเพื่ออ่านการ์ตูน รองรับ 1 - หลายพันรูป\nคู่มือการใช้ รวมถึงเวอร์ชันใหม่ๆเข้าไปดูได้ในที่ github ด้านล่าง\nคลิกเข้าไปที่แท็บ Repositories แล้วหาชื่อโปรแกรม\n\nBy nawakarit - เจช์ (วัดดงหมี)\nhttps://github.com/nawakarit-VOID\n© 2026", w)
	})
	// ============================================================================
	// จัดวาง UI
	// ============================================================================
	//TR := container.NewGridWrap(fyne.NewSize(49, 35), langSelect)
	//abbtn1 := container.NewGridWrap(fyne.NewSize(10, 35), abbtn)

	// ── Layout ──
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

/*
	labelt := NewLabel(tr, "We believe we are the fastest.")
	labelt.Alignment = fyne.TextAlignCenter

	label := NewLabel(tr, "Arrange the images in the folder first.\nSupports .jpg, .jpeg, .png, .webp, .bmp, and .tiff files.")
	label.Alignment = fyne.TextAlignCenter

	selectBtn1 := container.NewGridWrap(fyne.NewSize(150, 35), )
	clearBtn1 := container.NewGridWrap(fyne.NewSize(150, 35), clearBtn)
	convertBtn1 := container.NewGridWrap(fyne.NewSize(150, 35), convertBtn)

	input1 := container.NewBorder(
		nil,
		nil,
		nil,
		nil,
		container.NewCenter(container.NewHBox(selectBtn1, clearBtn1, convertBtn1)))

	prog := container.NewGridWrap(fyne.NewSize(395, 35), )
	TR := container.NewGridWrap(fyne.NewSize(49, 35), langSelect)
	abbtn1 := container.NewGridWrap(fyne.NewSize(10, 35), )

	ProgressTR := container.NewBorder(
		nil, nil, nil, nil,
		container.NewCenter(container.NewHBox(prog, TR, abbtn1)))

	top := container.NewVBox(

		labelt, label, input1, ProgressTR,
		container.NewCenter(),
	)

	content := container.NewBorder(
		top,               //บน
		nil,               //ล่าง
		nil,               // ซ้าย
		nil,               //ขวา
		 // กลาง
	)

	ui := container.NewStack(
		//bg, // พื้นหลัง
		//overlay,
		content, // UI ด้านบน
	)

	//w.SetContent(container.NewPadded(ui))
	w.SetContent(ui)
	w.Resize(fyne.NewSize(500, 700))
	//w.SetFixedSize(true)
	w.ShowAndRun()
}


*/
