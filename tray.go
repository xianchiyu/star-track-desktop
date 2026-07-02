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

// generateIconBytes 生成 32x32 星记托盘图标（ICO 格式）
func generateIconBytes() []byte {
	const size = 32
	img := image.NewRGBA(image.Rect(0, 0, size, size))

	// 深蓝灰不透明背景
	bg := color.RGBA{25, 35, 55, 255}
	draw.Draw(img, img.Bounds(), &image.Uniform{bg}, image.Point{}, draw.Src)

	// 新月：亮圆 + 偏移裁切圆
	cx, cy := 12.8, 16.0
	r := 11.2
	moon := color.RGBA{255, 240, 190, 255}
	cx2 := cx + r*0.45
	cy2 := cy - r*0.2
	r2 := r * 0.82

	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			fx, fy := float64(x), float64(y)
			dx, dy := fx-cx, fy-cy
			if dx*dx+dy*dy < r*r {
				img.Set(x, y, moon)
			}
			dx2, dy2 := fx-cx2, fy-cy2
			if dx2*dx2+dy2*dy2 < r2*r2 {
				img.Set(x, y, bg)
			}
		}
	}

	// 金色星星（右上角）
	starX, starY := 24, 9
	starR := 3
	star := color.RGBA{255, 200, 60, 255}
	for y := starY - starR; y <= starY+starR; y++ {
		for x := starX - starR; x <= starX+starR; x++ {
			if x < 0 || x >= size || y < 0 || y >= size {
				continue
			}
			dx, dy := float64(x-starX), float64(y-starY)
			if dx*dx+dy*dy < float64(starR*starR) {
				img.Set(x, y, star)
			}
		}
	}

	// 编码 PNG
	var pngBuf bytes.Buffer
	png.Encode(&pngBuf, img)
	pngData := pngBuf.Bytes()

	// 单尺寸 ICO（ICONDIR + ICONDIRENTRY + PNG data）
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
	systray.SetTooltip("星记 - 运行中")

	mOpen := systray.AddMenuItem("打开星记", "在浏览器中打开")
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
