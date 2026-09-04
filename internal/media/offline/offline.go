// Package offline 提供离线占位媒体生成（无外部服务依赖的本地实现）：
// 渐变图(PNG)/动画(GIF)/正弦波(WAV)/GLB 立方体/sprite sheet(PNG)，全部标准库实现。
package offline

import (
	"bytes"
	"context"
	"encoding/binary"
	"hash/fnv"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/png"
	"math"
	"strconv"

	"WorkBaby/internal/domain"
	"WorkBaby/internal/media"
	"WorkBaby/internal/pkg"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

// Generator 离线占位生成器：支持 image/video/audio/model3d/vfx 五种模态。
type Generator struct{}

// New 构造。
func New() *Generator { return &Generator{} }

// Kind 实现 media.Generator（占位生成器覆盖多种模态，Kind 返回占位值）。
func (g *Generator) Kind() domain.MediaKind { return domain.MediaKindImage }

// Generate 按 prompt + params 生成占位媒体；kind 由 params["kind"] 指定。
func (g *Generator) Generate(_ context.Context, prompt string, params map[string]any) (*media.Generated, error) {
	kind := domain.MediaKind(strOf(params["kind"]))
	switch kind {
	case domain.MediaKindImage:
		return imageGen(prompt, params)
	case domain.MediaKindVideo:
		return videoGen(prompt, params)
	case domain.MediaKindAudio:
		return audioGen(prompt, params)
	case domain.MediaKindModel3D:
		return model3dGen(params)
	case domain.MediaKindVFX:
		return vfxGen(prompt, params)
	default:
		return nil, pkg.New(9000, "unsupported media kind", string(kind))
	}
}

// palette 占位生成调色板。
var palette = []color.RGBA{
	{0x06, 0x6B, 0x8B, 0xFF}, {0x0E, 0xAD, 0x9E, 0xFF}, {0x4E, 0xC5, 0x49, 0xFF},
	{0xC2, 0x18, 0x5B, 0xFF}, {0xF8, 0xB5, 0x00, 0xFF}, {0xF0, 0x73, 0x05, 0xFF},
	{0x2D, 0x3E, 0x50, 0xFF}, {0x0F, 0x20, 0x27, 0xFF}, {0x6A, 0x11, 0xCD, 0xFF},
	{0xFF, 0x61, 0xA6, 0xFF},
}

// imageGen 渐变图 + 文字（PNG；basicfont 仅 ASCII，不绘制 prompt 避免 CJK 缺字）。
func imageGen(prompt string, params map[string]any) (*media.Generated, error) {
	w := intParam(params, "width", 512)
	h := intParam(params, "height", 512)
	seed := fnvHash(prompt)
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	c1 := palette[seed%len(palette)]
	c2 := palette[(seed/len(palette))%len(palette)]
	fillGradient(img, c1, c2)
	drawLabel(img, "WorkBaby", w/2, h/2)
	drawLabelSmall(img, "offline placeholder · "+strconv.Itoa(seed), w/2, h/2+22)

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, pkg.Wrap(9006, "encode png failed", err)
	}
	return &media.Generated{Bytes: buf.Bytes(), MimeType: "image/png", Ext: "png", Width: &w, Height: &h}, nil
}

// videoGen 动画 GIF（移动圆）。
func videoGen(prompt string, params map[string]any) (*media.Generated, error) {
	frames := clamp(intParam(params, "frames", 8), 2, 16)
	w := intParam(params, "width", 256)
	h := intParam(params, "height", 256)
	seed := fnvHash(prompt)
	var g gif.GIF
	for f := 0; f < frames; f++ {
		frame := gifFrame(seed, f, frames, w, h)
		g.Image = append(g.Image, frame)
		g.Delay = append(g.Delay, 10) // 100ms
	}
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, &g); err != nil {
		return nil, pkg.Wrap(9006, "encode gif failed", err)
	}
	return &media.Generated{Bytes: buf.Bytes(), MimeType: "image/gif", Ext: "gif", Width: &w, Height: &h}, nil
}

func gifFrame(seed, frame, total, w, h int) *image.Paletted {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	a := palette[(seed+frame)%len(palette)]
	b := palette[(seed+frame+3)%len(palette)]
	fillGradient(img, a, b)
	// 移动的圆
	t := float64(frame) / float64(total)
	cx := int(float64(w) * (0.15 + 0.7*t))
	cy := h / 2
	r := max(8, min(w, h)/10)
	white := color.RGBA{255, 255, 255, 200}
	drawCircle(img, cx, cy, r, white)

	p := image.NewPaletted(image.Rect(0, 0, w, h), gifPalette())
	draw.Draw(p, p.Rect, img, image.Point{}, draw.Src)
	return p
}

// audioGen WAV 正弦波（22050Hz，1s）。
func audioGen(prompt string, params map[string]any) (*media.Generated, error) {
	sampleRate := 22050
	durationMs := clamp(intParam(params, "duration_ms", 1000), 100, 5000)
	freq := 220 + fnvHash(prompt)%880
	numSamples := sampleRate * durationMs / 1000
	dataSize := numSamples * 2
	wav := make([]byte, 44+dataSize)
	binary.LittleEndian.PutUint32(wav[0:], 0x46464952) // RIFF
	binary.LittleEndian.PutUint32(wav[4:], uint32(36+dataSize))
	binary.LittleEndian.PutUint32(wav[8:], 0x45564157)  // WAVE
	binary.LittleEndian.PutUint32(wav[12:], 0x20746D66) // "fmt "
	binary.LittleEndian.PutUint32(wav[16:], 16)
	binary.LittleEndian.PutUint16(wav[20:], 1)
	binary.LittleEndian.PutUint16(wav[22:], 1)
	binary.LittleEndian.PutUint32(wav[24:], uint32(sampleRate))
	binary.LittleEndian.PutUint32(wav[28:], uint32(sampleRate*2))
	binary.LittleEndian.PutUint16(wav[32:], 2)
	binary.LittleEndian.PutUint16(wav[34:], 16)
	binary.LittleEndian.PutUint32(wav[36:], 0x61746164) // "data"
	binary.LittleEndian.PutUint32(wav[40:], uint32(dataSize))
	for i := 0; i < numSamples; i++ {
		env := 1.0 - float64(i)/float64(numSamples)
		sample := int16(math.Sin(2*math.Pi*float64(freq)*float64(i)/float64(sampleRate)) * 0.5 * 32767 * env)
		wav[44+i*2] = byte(sample)
		wav[44+i*2+1] = byte(sample >> 8)
	}
	return &media.Generated{Bytes: wav, MimeType: "audio/wav", Ext: "wav"}, nil
}

// model3dGen 最小 GLB 立方体。
func model3dGen(params map[string]any) (*media.Generated, error) {
	s := float32(floatParam(params, "size", 0.5))
	positions := []float32{
		-s, -s, -s, s, -s, -s, s, s, -s, -s, s, -s,
		-s, -s, s, s, -s, s, s, s, s, -s, s, s,
	}
	indices := []uint16{
		0, 1, 2, 0, 2, 3, 5, 4, 7, 5, 7, 6,
		4, 0, 3, 4, 3, 7, 1, 5, 6, 1, 6, 2,
		4, 5, 1, 4, 1, 0, 3, 2, 6, 3, 6, 7,
	}
	posLen := len(positions) * 4
	idxLen := len(indices) * 2
	binLen := posLen + idxLen
	bin := make([]byte, binLen)
	for i, p := range positions {
		binary.LittleEndian.PutUint32(bin[i*4:], math.Float32bits(p))
	}
	for i, ix := range indices {
		binary.LittleEndian.PutUint16(bin[posLen+i*2:], ix)
	}
	json := `{"asset":{"version":"2.0"},"scene":0,"scenes":[{"nodes":[0]}],` +
		`"nodes":[{"mesh":0}],"meshes":[{"primitives":[{"attributes":{"POSITION":0},"indices":1,"mode":4}]}],` +
		`"buffers":[{"byteLength":` + strconv.Itoa(binLen) + `}],` +
		`"bufferViews":[{"buffer":0,"byteOffset":0,"byteLength":` + strconv.Itoa(posLen) + `,"target":34962},` +
		`{"buffer":0,"byteOffset":` + strconv.Itoa(posLen) + `,"byteLength":` + strconv.Itoa(idxLen) + `,"target":34963}],` +
		`"accessors":[{"bufferView":0,"componentType":5126,"count":8,"type":"VEC3",` +
		`"min":[` + f2s(float64(-s)) + `,` + f2s(float64(-s)) + `,` + f2s(float64(-s)) + `],"max":[` + f2s(float64(s)) + `,` + f2s(float64(s)) + `,` + f2s(float64(s)) + `]},` +
		`{"bufferView":1,"componentType":5123,"count":36,"type":"SCALAR"}]}`
	glb := buildGLB([]byte(json), bin)
	return &media.Generated{Bytes: glb, MimeType: "model/gltf-binary", Ext: "glb"}, nil
}

func buildGLB(jsonBytes, bin []byte) []byte {
	jsonPad := (4 - len(jsonBytes)%4) % 4
	binPad := (4 - len(bin)%4) % 4
	total := 12 + 8 + len(jsonBytes) + jsonPad + 8 + len(bin) + binPad
	out := make([]byte, 0, total)
	out = append(out, "glTF"...)
	out = binary.LittleEndian.AppendUint32(out, 2)
	out = binary.LittleEndian.AppendUint32(out, uint32(total))
	out = binary.LittleEndian.AppendUint32(out, uint32(len(jsonBytes)+jsonPad))
	out = binary.LittleEndian.AppendUint32(out, 0x4E4F534A) // "JSON"
	out = append(out, jsonBytes...)
	out = append(out, make([]byte, jsonPad)...)
	out = binary.LittleEndian.AppendUint32(out, uint32(len(bin)+binPad))
	out = binary.LittleEndian.AppendUint32(out, 0x004E4942) // "BIN\0"
	out = append(out, bin...)
	out = append(out, make([]byte, binPad)...)
	return out
}

// vfxGen sprite sheet PNG（扩散圆环）。
func vfxGen(prompt string, params map[string]any) (*media.Generated, error) {
	frames := clamp(intParam(params, "frames", 8), 1, 16)
	cellW := intParam(params, "width", 128)
	cellH := intParam(params, "height", 128)
	seed := fnvHash(prompt)
	base := palette[seed%len(palette)]

	sheet := image.NewRGBA(image.Rect(0, 0, cellW*frames, cellH))
	for f := 0; f < frames; f++ {
		ox := f * cellW
		progress := float64(f) / float64(frames)
		radius := int(float64(cellW) * (0.1 + 0.4*progress))
		alpha := uint8(255 * (1 - progress*0.6))
		ring := color.RGBA{base.R, base.G, base.B, alpha}
		drawRing(sheet, ox+cellW/2, cellH/2, radius, max(2, cellW/24), ring)
		drawDot(sheet, ox+cellW/2, cellH/2, 4, color.RGBA{255, 255, 255, 220})
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, sheet); err != nil {
		return nil, pkg.Wrap(9006, "encode png failed", err)
	}
	w, h := cellW*frames, cellH
	return &media.Generated{Bytes: buf.Bytes(), MimeType: "image/png", Ext: "png", Width: &w, Height: &h}, nil
}

// ===== 绘图工具 =====

func fillGradient(img *image.RGBA, c1, c2 color.RGBA) {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	for y := 0; y < h; y++ {
		t := float64(y) / float64(max(1, h-1))
		r := lerp(c1.R, c2.R, t)
		g := lerp(c1.G, c2.G, t)
		b := lerp(c1.B, c2.B, t)
		row := color.RGBA{r, g, b, 0xFF}
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, row)
		}
	}
}

func drawLabel(img *image.RGBA, s string, cx, cy int) {
	d := &font.Drawer{Dst: img, Src: image.NewUniform(color.RGBA{255, 255, 255, 220}), Face: basicfont.Face7x13}
	w := d.MeasureString(s)
	d.Dot = fixed.P(cx-w.Ceil()/2, cy)
	d.DrawString(s)
}

func drawLabelSmall(img *image.RGBA, s string, cx, cy int) {
	d := &font.Drawer{Dst: img, Src: image.NewUniform(color.RGBA{255, 255, 255, 160}), Face: basicfont.Face7x13}
	w := d.MeasureString(s)
	d.Dot = fixed.P(cx-w.Ceil()/2, cy)
	d.DrawString(s)
}

func drawCircle(img *image.RGBA, cx, cy, r int, c color.RGBA) {
	for y := cy - r; y <= cy+r; y++ {
		for x := cx - r; x <= cx+r; x++ {
			dx, dy := x-cx, y-cy
			if dx*dx+dy*dy <= r*r {
				img.SetRGBA(x, y, c)
			}
		}
	}
}

func drawRing(img *image.RGBA, cx, cy, r, stroke int, c color.RGBA) {
	for y := cy - r - stroke; y <= cy+r+stroke; y++ {
		for x := cx - r - stroke; x <= cx+r+stroke; x++ {
			d := int(math.Hypot(float64(x-cx), float64(y-cy)))
			if d >= r-stroke/2 && d <= r+stroke/2 {
				img.SetRGBA(x, y, c)
			}
		}
	}
}

func drawDot(img *image.RGBA, cx, cy, r int, c color.RGBA) {
	drawCircle(img, cx, cy, r, c)
}

func gifPalette() color.Palette {
	p := make(color.Palette, 0, 256)
	// 前 10 色 = 调色板，其余为灰阶，保证量化后渐变平滑
	for _, c := range palette {
		p = append(p, c)
	}
	for i := 0; i < 246; i++ {
		v := uint8(i * 255 / 245)
		p = append(p, color.RGBA{v, v, v, 0xFF})
	}
	return p
}

func lerp(a, b uint8, t float64) uint8 {
	return uint8(float64(a) + (float64(b)-float64(a))*t)
}

func fnvHash(s string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(s))
	return int(h.Sum32())
}

func intParam(params map[string]any, key string, def int) int {
	if params == nil {
		return def
	}
	switch v := params[key].(type) {
	case int:
		return v
	case float64:
		return int(v)
	case string:
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func floatParam(params map[string]any, key string, def float64) float64 {
	if params == nil {
		return def
	}
	switch v := params[key].(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case string:
		if n, err := strconv.ParseFloat(v, 64); err == nil {
			return n
		}
	}
	return def
}

func strOf(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func clamp(v, lo, hi int) int {
	return max(lo, min(hi, v))
}

func f2s(v float64) string {
	return strconv.FormatFloat(v, 'g', -1, 64)
}
