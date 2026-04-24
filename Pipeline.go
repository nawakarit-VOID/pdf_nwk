// Copyright (c) 2026 Nawakarit
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License v3.0.
package main

import (
	"bytes"
	"image"
	"os"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
	"github.com/nfnt/resize"
)

// ─────────────────────────────────────────────
//  PIPELINE  (จากโค้ดที่ให้มา)
// ─────────────────────────────────────────────

func startPipeline(
	files []string,
	statuses []string,
	progress *widget.ProgressBar,
	status *widget.Label,
	cores int,
	output string,
	fileList *widget.List,
) {

	start := time.Now()
	total := len(files)

	// ── เพิ่มตรงนี้ ──
	stopTicker := make(chan struct{})
	go func() {
		t := time.NewTicker(50 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				fyne.Do(func() { fileList.Refresh() })
			case <-stopTicker:
				return
			}
		}
	}()

	jobs := make(chan Job, cores*4)
	decoded := make(chan Img, cores*4)
	resized := make(chan Img, cores*4)
	encoded := make(chan Encoded, cores*4)

	decodeWorkers := cores * 2
	resizeWorkers := cores
	encodeWorkers := cores

	var wgDecode sync.WaitGroup
	var wgResize sync.WaitGroup
	var wgEncode sync.WaitGroup

	// ---------- decode workers ----------
	for i := 0; i < decodeWorkers; i++ {
		wgDecode.Add(1)
		go func() {
			defer wgDecode.Done()
			for j := range jobs {
				f, err := os.Open(j.path)
				if err != nil {
					continue
				}
				img, _, err := image.Decode(f)
				f.Close()
				if err != nil {
					continue
				}
				decoded <- Img{index: j.index, img: img}
				updateStatus(j.index, "🔀 decoding", statuses)
			}
		}()
	}

	// ---------- resize workers ----------
	for i := 0; i < resizeWorkers; i++ {
		wgResize.Add(1)
		go func() {
			defer wgResize.Done()
			for im := range decoded {
				b := im.img.Bounds()
				if b.Dx() > 2480 {
					im.img = resize.Resize(2480, 0, im.img, resize.Bilinear)
				}
				resized <- im
				updateStatus(im.index, "↔️ resizing", statuses)
			}
		}()
	}

	// ---------- encode workers ----------
	for i := 0; i < encodeWorkers; i++ {
		wgEncode.Add(1)
		go func() {
			defer wgEncode.Done()
			for im := range resized {
				buf := jpegPool.Get().(*bytes.Buffer)
				buf.Reset()
				encodeJPEG(buf, im.img)
				b := im.img.Bounds()
				encoded <- Encoded{
					index: im.index,
					buf:   buf,
					w:     float64(b.Dx()) * 0.264583,
					h:     float64(b.Dy()) * 0.264583,
				}
				updateStatus(im.index, "🔄 encoding", statuses)
			}
		}()
	}

	// ---------- feed jobs ----------
	go func() {
		for i, f := range files {
			jobs <- Job{index: i, path: f}
		}
		close(jobs)
	}()

	// ---------- close channels ----------
	go func() { wgDecode.Wait(); close(decoded) }()
	go func() { wgResize.Wait(); close(resized) }()
	go func() { wgEncode.Wait(); close(encoded) }()

	writePDF(encoded, total, statuses, progress, status, start, output) //, fileList)

	close(stopTicker)
}
