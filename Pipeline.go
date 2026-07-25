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
	"golang.org/x/image/draw"
)

// ─────────────────────────────────────────────
//  PIPELINE
// ─────────────────────────────────────────────

func startPipeline(
	files []string,
	fe *FolderEntry,
	progress *widget.ProgressBar,
	status *widget.Label,
	cores int,
	output string,
	fileList *widget.List,
) error {

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
					decoded <- Img{index: j.index, img: nil, err: err}
					fe.UpdateStatus(j.index, "❌ open error")
					continue
				}
				img, _, err := image.Decode(f)
				f.Close()
				if err != nil {
					decoded <- Img{index: j.index, img: nil, err: err}
					fe.UpdateStatus(j.index, "❌ decode error")
					continue
				}
				decoded <- Img{index: j.index, img: img}
				fe.UpdateStatus(j.index, "🔀 decoding")
			}
		}()
	}

	// ---------- resize workers ----------
	for i := 0; i < resizeWorkers; i++ {
		wgResize.Add(1)
		go func() {
			defer wgResize.Done()
			for im := range decoded {
				if im.err != nil {
					resized <- im
					continue
				}
				b := im.img.Bounds()
				if b.Dx() > 2480 {
					newW := 2480
					newH := int(float64(b.Dy()) * (2480.0 / float64(b.Dx())))
					dst := image.NewRGBA(image.Rect(0, 0, newW, newH))
					draw.BiLinear.Scale(dst, dst.Bounds(), im.img, b, draw.Over, nil)
					im.img = dst
				}
				resized <- im
				fe.UpdateStatus(im.index, "↔️ resizing")
			}
		}()
	}

	// ---------- encode workers ----------
	for i := 0; i < encodeWorkers; i++ {
		wgEncode.Add(1)
		go func() {
			defer wgEncode.Done()
			for im := range resized {
				if im.err != nil {
					encoded <- Encoded{index: im.index, err: im.err}
					continue
				}
				buf := jpegPool.Get().(*bytes.Buffer)
				buf.Reset()
				err := encodeJPEG(buf, im.img)
				if err != nil {
					encoded <- Encoded{index: im.index, err: err}
					fe.UpdateStatus(im.index, "❌ encode error")
					continue
				}
				b := im.img.Bounds()
				encoded <- Encoded{
					index: im.index,
					buf:   buf,
					w:     float64(b.Dx()) * 0.264583,
					h:     float64(b.Dy()) * 0.264583,
				}
				fe.UpdateStatus(im.index, "🔄 encoding")
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

	err := writePDF(encoded, total, fe, progress, status, start, output)

	close(stopTicker)
	return err
}
