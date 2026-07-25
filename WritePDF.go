// Copyright (c) 2026 Nawakarit
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License v3.0.
package main

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
	"github.com/jung-kurt/gofpdf"
)

// ฟังก์ชันสำหรับการเขียนไฟล์ PDF โดยรับข้อมูลภาพที่ถูก encode แล้วจาก channel
// และจัดเรียงตามลำดับ index เพื่อให้ภาพอยู่ในลำดับที่ถูกต้องใน PDF จากนั้นใช้ gofpdf
// ในการสร้าง PDF และเพิ่มภาพลงไปทีละหน้า พร้อมอัปเดต progress bar และสถานะการทำงานใน UI
// ─────────────────────────────────────────────
//  writePDF
// ─────────────────────────────────────────────

func writePDF(
	ctx context.Context,
	in <-chan Encoded,
	total int,
	fe *FolderEntry,
	progress *widget.ProgressBar,
	status *widget.Label,
	start time.Time,
	output string,
) error {

	// ใช้ custom size — จะกำหนด page size ต่อหน้าตามขนาดภาพจริง
	pdf := gofpdf.NewCustom(&gofpdf.InitType{
		UnitStr: "mm",
		Size:    gofpdf.SizeType{Wd: 210, Ht: 297}, // placeholder, จะ override ต่อหน้า
	})
	pdf.SetAutoPageBreak(false, 0)
	pdf.SetMargins(0, 0, 0)

	buffer := map[int]Encoded{}
	next := 0
	done := 0
	hasPages := false

	for r := range in {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		buffer[r.index] = r
		for {
			res, ok := buffer[next]
			if !ok {
				break
			}
			delete(buffer, next)

			if res.err != nil {
				fe.UpdateStatus(next, "❌ failed")
				next++
				done++

				elapsed := time.Since(start).Seconds()
				var speed, eta float64
				if elapsed > 0 {
					speed = float64(done) / elapsed
					per := elapsed / float64(done)
					eta = per * float64(total-done)
				}

				fyne.Do(func() {
					progress.SetValue(float64(done) / float64(total))
					status.SetText(fmt.Sprintf(
						"🔀 %d / %d images   🎨 %.1f img/s   ⏱ ETA %.1fs",
						done, total, speed, eta,
					))
				})
				continue
			}

			imgW := res.w // mm (pixel * 0.264583 @ 96dpi)
			imgH := res.h

			// เพิ่มหน้าที่มีขนาดเท่ากับภาพพอดี — ไม่มีขอบขาว
			pdf.AddPageFormat("P", gofpdf.SizeType{Wd: imgW, Ht: imgH})
			hasPages = true

			name := fmt.Sprintf("img%d", next)
			opt := gofpdf.ImageOptions{ImageType: "JPG"}
			pdf.RegisterImageOptionsReader(name, opt, bytes.NewReader(res.buf.Bytes()))

			// วางภาพที่ (0,0) เต็มหน้า ไม่มีขอบ
			pdf.ImageOptions(name, 0, 0, imgW, imgH, false, opt, 0, "")

			jpegPool.Put(res.buf)

			fe.UpdateStatus(next, "✅ done")

			next++
			done++

			elapsed := time.Since(start).Seconds()
			var speed, eta float64
			if elapsed > 0 {
				speed = float64(done) / elapsed
				per := elapsed / float64(done)
				eta = per * float64(total-done)
			}

			fyne.Do(func() {
				progress.SetValue(float64(done) / float64(total))
				status.SetText(fmt.Sprintf(
					"🔀 %d / %d images   🎨 %.1f img/s   ⏱ ETA %.1fs",
					done, total, speed, eta,
				))
			})
		}
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	if !hasPages {
		return fmt.Errorf("no valid images to write to PDF")
	}

	err := pdf.OutputFileAndClose(output)
	if err != nil {
		return err
	}

	fyne.Do(func() {
		elapsed := time.Since(start).Seconds()
		status.SetText(fmt.Sprintf(
			"✅Done! ▶️ %d images in %.1fs 🕒 (%.1f img/s)",
			total, elapsed, float64(total)/elapsed,
		))
	})
	return nil
}
