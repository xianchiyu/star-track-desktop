package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/draw"
	"image/png"

	"github.com/getlantern/systray"
)

var trayURL string

// generateIconBytes 生成 32x32 星轨图标（ICO 格式）
func generateIconBytes() []byte {
	const size = 32
	img := image.NewRGBA(image.Rect(0, 0, size, size))

	// 深蓝背景
	draw.Draw(img, img.Bounds(), &image.Uniform{color.RGBA{6, 10, 20, 255}}, image.Point{}, draw.Src)

	// 新月（两个圆裁切）
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			dx, dy := x-12, y-13
			if dx*dx+dy*dy < 55 {
				img.Set(x, y, color.RGBA{255, 230, 200, 255})
			}
			dx2, dy2 := x-16, y-10
			if dx2*dx2+dy2*dy2 < 45 {
				img.Set(x, y, color.RGBA{6, 10, 20, 255})
			}
		}
	}

	// 金色星星
	img.Set(22, 7, color.RGBA{255, 215, 0, 255})
	img.Set(23, 7, color.RGBA{255, 215, 0, 255})
	img.Set(22, 8, color.RGBA{255, 215, 0, 255})
	img.Set(23, 8, color.RGBA{255, 215, 0, 255})

	// 紫色小星
	img.Set(7, 22, color.RGBA{147, 112, 219, 255})
	img.Set(8, 23, color.RGBA{147, 112, 219, 255})

	// 编码 PNG
	var pngBuf bytes.Buffer
	png.Encode(&pngBuf, img)
	pngData := pngBuf.Bytes()

	// 组装 ICO（ICONDIR + ICONDIRENTRY + PNG data）
	var ico bytes.Buffer
	binary.Write(&ico, binary.LittleEndian, uint16(0))  // reserved
	binary.Write(&ico, binary.LittleEndian, uint16(1))  // type: icon
	binary.Write(&ico, binary.LittleEndian, uint16(1))  // count: 1
	ico.WriteByte(byte(size))                           // width
	ico.WriteByte(byte(size))                           // height
	ico.WriteByte(0)                                    // color count
	ico.WriteByte(0)                                    // reserved
	binary.Write(&ico, binary.LittleEndian, uint16(1))  // planes
	binary.Write(&ico, binary.LittleEndian, uint16(32)) // bpp
	binary.Write(&ico, binary.LittleEndian, uint32(len(pngData)))
	binary.Write(&ico, binary.LittleEndian, uint32(22)) // offset = 6 + 16
	ico.Write(pngData)

	return ico.Bytes()
}

// startTray 启动系统托盘（阻塞，直到用户选择退出）
func startTray(url string) {
	trayURL = url
	systray.Run(onTrayReady, onTrayExit)
}

func onTrayReady() {
	systray.SetIcon(generateIconBytes())
	systray.SetTitle("")
	systray.SetTooltip("星轨 · 心事星空 - 运行中")

	mOpen := systray.AddMenuItem("打开星轨", "在浏览器中打开")
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("退出", "退出程序")

	go func() {
		for {
			select {
			case <-mOpen.ClickedCh:
				openBrowser("http://" + trayURL)
			case <-mQuit.ClickedCh:
				systray.Quit()
				return
			}
		}
	}()
}

func onTrayExit() {
	// 程序即将退出，无需额外清理
}
