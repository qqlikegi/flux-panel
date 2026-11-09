package main

// 登录验证码，兼容前端 tianai-captcha (TAC) 组件的接口格式。
// 支持 SLIDER（滑块拼图）、ROTATE（旋转）、CONCAT（滑动还原）、WORD_IMAGE_CLICK（按顺序点选字符）。

import (
	"bytes"
	"embed"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"math"
	"math/rand"
	"sync"
	"time"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

//go:embed assets/captcha
var captchaFS embed.FS

const (
	capW, capH      = 600, 360
	captchaTTL      = 2 * time.Minute
	captchaTolerate = 0.02 // 滑动类允许的误差（占背景宽度的比例）
)

var captchaTypes = []string{"SLIDER", "WORD_IMAGE_CLICK", "ROTATE", "CONCAT"}

type captchaEntry struct {
	typ     string
	percent float64      // 滑动类：正确位置占比
	points  [][2]float64 // 点选类：按顺序的字符中心（原图坐标）
	expire  time.Time
}

var (
	captchaStore  sync.Map // id -> *captchaEntry
	captchaTokens sync.Map // 校验通过的 id -> 过期时间（登录时二次校验，单次有效）
	captchaOnce   sync.Once
)

func captchaJanitor() {
	for range time.Tick(time.Minute) {
		now := time.Now()
		captchaStore.Range(func(k, v any) bool {
			if now.After(v.(*captchaEntry).expire) {
				captchaStore.Delete(k)
			}
			return true
		})
		captchaTokens.Range(func(k, v any) bool {
			if now.After(v.(time.Time)) {
				captchaTokens.Delete(k)
			}
			return true
		})
	}
}

func captchaCheck(c *Ctx) any {
	if v, _ := getConfigValue("captcha_enabled"); v == "true" {
		return ok(1)
	}
	return ok(0)
}

func captchaGenerate(c *Ctx) any {
	captchaOnce.Do(func() { go captchaJanitor() })
	typ, _ := getConfigValue("captcha_type")
	valid := false
	for _, t := range captchaTypes {
		if t == typ {
			valid = true
		}
	}
	if !valid {
		typ = captchaTypes[rand.Intn(len(captchaTypes))]
	}
	bg := randomBackground()
	entry := &captchaEntry{typ: typ, expire: time.Now().Add(captchaTTL)}
	var tpl image.Image
	data := any(nil)
	switch typ {
	case "SLIDER":
		tpl, entry.percent = genSlider(bg)
	case "ROTATE":
		tpl, entry.percent = genRotate(bg)
	case "CONCAT":
		var randomY int
		entry.percent, randomY = genConcat(bg)
		data = map[string]any{"randomY": randomY}
	default:
		tpl, entry.points = genWordClick(bg)
	}
	id := simpleUUID()
	captchaStore.Store(id, entry)

	vo := map[string]any{
		"type":                  typ,
		"backgroundImage":       encodeJPEG(bg),
		"templateImage":         nil,
		"backgroundImageTag":    "default",
		"templateImageTag":      "default",
		"backgroundImageWidth":  capW,
		"backgroundImageHeight": capH,
		"templateImageWidth":    nil,
		"templateImageHeight":   nil,
		"data":                  data,
	}
	if tpl != nil {
		vo["templateImage"] = encodePNG(tpl)
		vo["templateImageWidth"] = tpl.Bounds().Dx()
		vo["templateImageHeight"] = tpl.Bounds().Dy()
	}
	return rawJSON{map[string]any{"id": id, "captcha": vo}}
}

type trackPoint struct {
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
	T    float64 `json:"t"`
	Type string  `json:"type"`
}

func captchaVerify(c *Ctx) any {
	var d struct {
		ID   string `json:"id"`
		Data struct {
			BgImageWidth  float64      `json:"bgImageWidth"`
			BgImageHeight float64      `json:"bgImageHeight"`
			TrackList     []trackPoint `json:"trackList"`
		} `json:"data"`
	}
	c.bind(&d)
	failResp := rawJSON{map[string]any{"code": 4001, "msg": "基础校验失败", "data": nil}}
	v, found := captchaStore.LoadAndDelete(d.ID) // 每个验证码只能校验一次
	if !found || time.Now().After(v.(*captchaEntry).expire) {
		return rawJSON{map[string]any{"code": 4000, "msg": "验证码已失效", "data": nil}}
	}
	e := v.(*captchaEntry)
	tracks := d.Data.TrackList
	if len(tracks) < 2 || d.Data.BgImageWidth <= 0 || tracks[len(tracks)-1].T < 200 {
		return failResp
	}
	passed := false
	if e.typ == "WORD_IMAGE_CLICK" {
		var clicks []trackPoint
		for _, t := range tracks {
			if t.Type == "click" {
				clicks = append(clicks, t)
			}
		}
		if len(clicks) == len(e.points) && d.Data.BgImageHeight > 0 {
			passed = true
			for i, p := range e.points {
				x := clicks[i].X * capW / d.Data.BgImageWidth
				y := clicks[i].Y * capH / d.Data.BgImageHeight
				if math.Hypot(x-p[0], y-p[1]) > 30 {
					passed = false
					break
				}
			}
		}
	} else {
		// 滑动类：最后一个轨迹点的位移 / 背景宽度（旋转类前端传的是滑轨长度）
		pct := tracks[len(tracks)-1].X / d.Data.BgImageWidth
		passed = math.Abs(pct-e.percent) <= captchaTolerate
	}
	if !passed {
		return failResp
	}
	captchaTokens.Store(d.ID, time.Now().Add(captchaTTL))
	return rawJSON{map[string]any{"code": 200, "msg": "OK", "data": map[string]any{"validToken": d.ID}}}
}

// captchaSecondaryVerify 登录时校验验证码令牌（单次有效）
func captchaSecondaryVerify(token string) bool {
	v, found := captchaTokens.LoadAndDelete(token)
	return found && time.Now().Before(v.(time.Time))
}

// ---------- 图片生成 ----------

func loadAsset(name string) image.Image {
	b, err := captchaFS.ReadFile("assets/captcha/" + name)
	if err != nil {
		panic(err)
	}
	img, _, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		panic(err)
	}
	return img
}

func randomBackground() *image.RGBA {
	src := loadAsset(fmt.Sprintf("bgimages/%d.jpg", rand.Intn(26)+1))
	dst := image.NewRGBA(image.Rect(0, 0, capW, capH))
	sb := src.Bounds()
	if sb.Dx() == capW && sb.Dy() == capH {
		draw.Draw(dst, dst.Bounds(), src, sb.Min, draw.Src)
		return dst
	}
	for y := 0; y < capH; y++ {
		for x := 0; x < capW; x++ {
			dst.Set(x, y, src.At(sb.Min.X+x*sb.Dx()/capW, sb.Min.Y+y*sb.Dy()/capH))
		}
	}
	return dst
}

func encodeJPEG(img image.Image) string {
	var buf bytes.Buffer
	_ = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85})
	return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}

func encodePNG(img image.Image) string {
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}

func randRange(min, max int) int {
	if max <= min {
		return min
	}
	return min + rand.Intn(max-min+1)
}

// genSlider 滑块拼图：从背景上抠出拼图块，背景上留下阴影缺口
func genSlider(bg *image.RGBA) (image.Image, float64) {
	i := rand.Intn(10) + 1
	active := loadAsset(fmt.Sprintf("slide/%d/1.png", i)) // 拼图块边框
	fixedT := loadAsset(fmt.Sprintf("slide/%d/2.png", i)) // 缺口阴影
	fb := fixedT.Bounds()
	tw, th := fb.Dx(), fb.Dy()
	x := randRange(tw+5, capW-tw-10)
	y := randRange(0, capH-th)

	piece := image.NewRGBA(image.Rect(0, 0, tw, th))
	for py := 0; py < th; py++ {
		for px := 0; px < tw; px++ {
			if _, _, _, a := fixedT.At(fb.Min.X+px, fb.Min.Y+py).RGBA(); a > 0 {
				piece.Set(px, py, bg.At(x+px, y+py))
			}
		}
	}
	draw.Draw(bg, image.Rect(x, y, x+tw, y+th), fixedT, fb.Min, draw.Over)
	draw.Draw(piece, piece.Bounds(), active, active.Bounds().Min, draw.Over)

	tpl := image.NewRGBA(image.Rect(0, 0, tw, capH))
	draw.Draw(tpl, image.Rect(0, y, tw, y+th), piece, image.Point{}, draw.Src)
	return tpl, float64(x) / capW
}

// genRotate 旋转：中心圆形区域被遮盖，模板是逆时针旋转过的圆形图块，用户需顺时针转回
func genRotate(bg *image.RGBA) (image.Image, float64) {
	const r = 100.0
	deg := randRange(40, 320)
	cx, cy := float64(capW)/2, float64(capH)/2
	tpl := image.NewRGBA(image.Rect(0, 0, capH, capH))
	tc := float64(capH) / 2
	rad := float64(deg) * math.Pi / 180
	sin, cos := math.Sin(rad), math.Cos(rad)
	for y := 0; y < capH; y++ {
		for x := 0; x < capH; x++ {
			dx, dy := float64(x)+0.5-tc, float64(y)+0.5-tc
			d := math.Hypot(dx, dy)
			if d > r {
				continue
			}
			// 模板像素 p 显示原图中 R(deg)·p 处的内容
			sx, sy := dx*cos-dy*sin+cx, dx*sin+dy*cos+cy
			c := bilinear(bg, sx, sy)
			if d > r-3 {
				c = color.RGBA{255, 255, 255, 255}
			}
			tpl.Set(x, y, c)
		}
	}
	// 遮盖背景中心圆
	for y := int(cy - r - 1); y <= int(cy+r+1); y++ {
		for x := int(cx - r - 1); x <= int(cx+r+1); x++ {
			d := math.Hypot(float64(x)+0.5-cx, float64(y)+0.5-cy)
			if d <= r {
				o := bg.RGBAAt(x, y)
				bg.SetRGBA(x, y, color.RGBA{uint8(int(o.R) * 35 / 100), uint8(int(o.G) * 35 / 100), uint8(int(o.B) * 35 / 100), 255})
			}
		}
	}
	return tpl, float64(deg) / 360
}

func bilinear(img *image.RGBA, x, y float64) color.RGBA {
	x -= 0.5
	y -= 0.5
	x0, y0 := int(math.Floor(x)), int(math.Floor(y))
	fx, fy := x-float64(x0), y-float64(y0)
	at := func(px, py int) color.RGBA {
		b := img.Bounds()
		if px < b.Min.X {
			px = b.Min.X
		}
		if py < b.Min.Y {
			py = b.Min.Y
		}
		if px >= b.Max.X {
			px = b.Max.X - 1
		}
		if py >= b.Max.Y {
			py = b.Max.Y - 1
		}
		return img.RGBAAt(px, py)
	}
	c00, c10, c01, c11 := at(x0, y0), at(x0+1, y0), at(x0, y0+1), at(x0+1, y0+1)
	mix := func(a, b, c, d uint8) uint8 {
		v := float64(a)*(1-fx)*(1-fy) + float64(b)*fx*(1-fy) + float64(c)*(1-fx)*fy + float64(d)*fx*fy
		return uint8(math.Round(v))
	}
	return color.RGBA{mix(c00.R, c10.R, c01.R, c11.R), mix(c00.G, c10.G, c01.G, c11.G), mix(c00.B, c10.B, c01.B, c11.B), mix(c00.A, c10.A, c01.A, c11.A)}
}

// genConcat 滑动还原：图片上半部分水平错位，用户拖动还原
func genConcat(bg *image.RGBA) (float64, int) {
	randomY := randRange(capH/4, capH*3/4)
	shift := randRange(60, 470)
	top := capH - randomY
	row := make([]color.RGBA, capW)
	for y := 0; y < top; y++ {
		for x := 0; x < capW; x++ {
			row[x] = bg.RGBAAt((x+shift)%capW, y)
		}
		for x := 0; x < capW; x++ {
			bg.SetRGBA(x, y, row[x])
		}
	}
	return float64(shift) / capW, randomY
}

const clickChars = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

var (
	fontOnce sync.Once
	bigFace  font.Face
	tipFace  font.Face
)

func loadFaces() {
	f, err := opentype.Parse(gobold.TTF)
	if err != nil {
		panic(err)
	}
	bigFace, _ = opentype.NewFace(f, &opentype.FaceOptions{Size: 44, DPI: 72, Hinting: font.HintingFull})
	tipFace, _ = opentype.NewFace(f, &opentype.FaceOptions{Size: 26, DPI: 72, Hinting: font.HintingFull})
}

// glyphMask 把单个字符渲染到 size×size 的透明图中央
func glyphMask(face font.Face, ch string, size int) *image.Alpha {
	mask := image.NewAlpha(image.Rect(0, 0, size, size))
	d := &font.Drawer{Dst: mask, Src: image.Opaque, Face: face}
	adv := d.MeasureString(ch)
	m := face.Metrics()
	h := m.Ascent + m.Descent
	d.Dot = fixed.Point26_6{
		X: fixed.I(size)/2 - adv/2,
		Y: fixed.I(size)/2 - h/2 + m.Ascent,
	}
	d.DrawString(ch)
	return mask
}

// genWordClick 背景上随机放置 5 个字符，提示图给出其中 4 个，按顺序点击
func genWordClick(bg *image.RGBA) (image.Image, [][2]float64) {
	fontOnce.Do(loadFaces)
	perm := rand.Perm(len(clickChars))
	chars := make([]string, 5)
	for i := range chars {
		chars[i] = string(clickChars[perm[i]])
	}
	var centers [][2]float64
	const box = 64
	for len(centers) < len(chars) {
		cx := float64(randRange(45, capW-45))
		cy := float64(randRange(45, capH-45))
		tooClose := false
		for _, p := range centers {
			if math.Hypot(p[0]-cx, p[1]-cy) < 80 {
				tooClose = true
				break
			}
		}
		if !tooClose {
			centers = append(centers, [2]float64{cx, cy})
		}
	}
	palette := []color.RGBA{{255, 230, 60, 255}, {120, 255, 140, 255}, {110, 210, 255, 255}, {255, 140, 200, 255}, {255, 255, 255, 255}}
	for i, ch := range chars {
		mask := glyphMask(bigFace, ch, box)
		angle := float64(randRange(-35, 35)) * math.Pi / 180
		sin, cos := math.Sin(angle), math.Cos(angle)
		col := palette[rand.Intn(len(palette))]
		cx, cy := centers[i][0], centers[i][1]
		for y := -box / 2; y < box/2; y++ {
			for x := -box / 2; x < box/2; x++ {
				// 反向旋转取字形像素
				sx := float64(x)*cos + float64(y)*sin + box/2
				sy := -float64(x)*sin + float64(y)*cos + box/2
				px, py := int(sx), int(sy)
				if px < 0 || py < 0 || px >= box || py >= box {
					continue
				}
				a := mask.AlphaAt(px, py).A
				if a == 0 {
					continue
				}
				bx, by := int(cx)+x, int(cy)+y
				if bx < 0 || by < 0 || bx >= capW || by >= capH {
					continue
				}
				// 阴影 + 文字，保证在任何背景上都看得清
				for _, layer := range []struct {
					dx, dy int
					c      color.RGBA
				}{{2, 2, color.RGBA{0, 0, 0, 255}}, {0, 0, col}} {
					tx, ty := bx+layer.dx, by+layer.dy
					if tx >= capW || ty >= capH {
						continue
					}
					o := bg.RGBAAt(tx, ty)
					f := float64(a) / 255
					bg.SetRGBA(tx, ty, color.RGBA{
						uint8(float64(layer.c.R)*f + float64(o.R)*(1-f)),
						uint8(float64(layer.c.G)*f + float64(o.G)*(1-f)),
						uint8(float64(layer.c.B)*f + float64(o.B)*(1-f)), 255,
					})
				}
			}
		}
	}
	// 提示图：按点击顺序列出前 4 个字符
	tip := image.NewRGBA(image.Rect(0, 0, 150, 40))
	for i := 0; i < 4; i++ {
		m := glyphMask(tipFace, chars[i], 36)
		draw.DrawMask(tip, image.Rect(i*37+2, 2, i*37+38, 38), image.NewUniform(color.RGBA{40, 40, 40, 255}), image.Point{}, m, image.Point{}, draw.Over)
	}
	return tip, centers[:4]
}
