package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"

	"github.com/getlantern/systray"
)

var trayURL string

// generateIconBytes 生成星记托盘图标（ICO 格式，含 32x32 + 16x16 两种尺寸）
func generateIconBytes() []byte {
	icon32 := drawStarIcon(32)
	icon16 := drawStarIcon(16)

	png32 := encodePNG(icon32)
	png16 := encodePNG(icon16)

	// 多尺寸 ICO: ICONDIR(6) + 2×ICONDIRENTRY(16) + png32 + png16
	var ico bytes.Buffer
	binary.Write(&ico, binary.LittleEndian, uint16(0)) // reserved
	binary.Write(&ico, binary.LittleEndian, uint16(1)) // type: icon
	binary.Write(&ico, binary.LittleEndian, uint16(2)) // count: 2

	// 第 1 张: 32x32
	ico.WriteByte(32)
	ico.WriteByte(32)
	ico.WriteByte(0)
	ico.WriteByte(0)
	binary.Write(&ico, binary.LittleEndian, uint16(1))
	binary.Write(&ico, binary.LittleEndian, uint16(32))
	binary.Write(&ico, binary.LittleEndian, uint32(len(png32)))
	binary.Write(&ico, binary.LittleEndian, uint32(6+16*2)) // offset = 38
	ico.Write(png32)

	// 第 2 张: 16x16
	ico.WriteByte(16)
	ico.WriteByte(16)
	ico.WriteByte(0)
	ico.WriteByte(0)
	binary.Write(&ico, binary.LittleEndian, uint16(1))
	binary.Write(&ico, binary.LittleEndian, uint16(32))
	binary.Write(&ico, binary.LittleEndian, uint32(len(png16)))
	binary.Write(&ico, binary.LittleEndian, uint32(6+16*2+len(png32))) // offset
	ico.Write(png16)

	return ico.Bytes()
}

// drawStarIcon 绘制星空图标：透明背景 + 大号亮色新月 + 一颗金星
func drawStarIcon(size int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, size, size))

	// 透明背景（不填充，默认零值即透明）

	// 计算月亮参数，按图标比例缩放
	cx := float64(size) * 0.42
	cy := float64(size) * 0.45
	r := float64(size) * 0.35

	// 月亮亮色
	moon := color.RGBA{255, 235, 180, 255}
	// 裁切圆（透明）
	transparent := color.RGBA{0, 0, 0, 0}

	// 新月：画一个亮圆，再用偏移的透明圆裁出月牙
	cx2 := cx + r*0.5
	cy2 := cy - r*0.25
	r2 := r * 0.85

	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			fx, fy := float64(x), float64(y)
			dx, dy := fx-cx, fy-cy
			if dx*dx+dy*dy < r*r {
				img.Set(x, y, moon)
			}
			dx2, dy2 := fx-cx2, fy-cy2
			if dx2*dx2+dy2*dy2 < r2*r2 {
				img.Set(x, y, transparent)
			}
		}
	}

	// 金色星星（右上角）
	starX := int(float64(size) * 0.72)
	starY := int(float64(size) * 0.28)
	starR := size / 8
	if starR < 1 {
		starR = 1
	}
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

	return img
}

func encodePNG(img image.Image) []byte {
	var buf bytes.Buffer
	png.Encode(&buf, img)
	return buf.Bytes()
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
