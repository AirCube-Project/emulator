package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/go-playground/colors"
	"github.com/hexops/vecty"
	"github.com/hexops/vecty/elem"
	"github.com/hexops/vecty/prop"
	"github.com/llgcode/draw2d/draw2dimg"
	"github.com/markfarnan/go-canvas/canvas"
	"golang.org/x/text/encoding/charmap"
	"image"
	"image/color"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"strconv"
	"syscall/js"
	"time"
)

const URLPrefix = "http://localhost:8080/api/v1"
const WSURL = "ws://localhost:8080/ws"

const screenWidth = 160
const screenHeight = 120

//const URLPrefix = "https://api.aircube.tech/api/v1"
//const WSURL = "wss://api.aircube.tech/ws"

type ScreenContent struct {
	points []byte
	//todo: text
}

var screens []ScreenContent
var cvs []*canvas.Canvas2d
var descriptors []ScreenDescriptor
var screenLists [][]ListItem
var active int

var powerOn = false
var lightColor colors.Color
var flipped = false

type ScreenDescriptor struct {
	navigable bool
	topY      int
	topLine   int
	selected  int
	count     int
	list      bool
	title     *string
}

type ScreenView struct {
	vecty.Core
	id int
}

func (p *ScreenView) Render() vecty.ComponentOrHTML {
	return elem.Div(
		vecty.Markup(
			vecty.Class("screen"+strconv.Itoa(p.id)),
			vecty.MarkupIf(powerOn && active == p.id, vecty.Class("selected"))),
		elem.Canvas(
			vecty.Markup(
				prop.ID("canvas"+strconv.Itoa(p.id)),
				vecty.Style("background", "black"),
				vecty.Property("width", strconv.Itoa(screenWidth)),
				vecty.Property("height", strconv.Itoa(screenHeight)),
			),
		),
	)
}

type FlipButton struct {
	vecty.Core
}

const TYPE_TAP = 0
const TYPE_FLIP = 1
const TYPE_CHANGE = 2

func (p *FlipButton) Render() vecty.ComponentOrHTML {
	var ch = "\uF0AA"
	if flipped {
		ch = "\uF0AB"
	}

	fd := 0
	if flipped {
		fd = 1
	}

	return elem.Div(vecty.Markup(vecty.Class("centered")), elem.Div(vecty.Markup(vecty.Class("flip-button"), &vecty.EventListener{Name: "click", Listener: func(event *vecty.Event) {
		flipped = !flipped
		data, _ := json.Marshal(CubeInfo{Type: TYPE_FLIP, Cube: 1, State: &fd})
		SendToServer(string(data))
		UpdateScreens()
		vecty.Rerender(emulator)
	}},
	), vecty.Text(ch)))
}

type LeftButton struct {
	vecty.Core
}

func (p *LeftButton) Render() vecty.ComponentOrHTML {
	return elem.Data(vecty.Markup(vecty.Class("fa-button"),
		vecty.Class("left"), &vecty.EventListener{Name: "click", Listener: func(event *vecty.Event) {
			if powerOn {
				temp := active
				temp--
				if temp < 0 {
					temp = 4 - 1
				}
				data, _ := json.Marshal(CubeInfo{Type: TYPE_CHANGE, Cube: 1, Screen: &temp})
				SendToServer(string(data))
			}
		}},
	), vecty.Text("\uF053"))
}

type RightButton struct {
	vecty.Core
}

func (p *RightButton) Render() vecty.ComponentOrHTML {
	return elem.Data(vecty.Markup(
		vecty.Class("fa-button"),
		vecty.Class("right"),
		&vecty.EventListener{Name: "click", Listener: func(event *vecty.Event) {
			if powerOn {
				temp := active
				temp++
				temp = temp % 4
				data, _ := json.Marshal(CubeInfo{Type: TYPE_CHANGE, Cube: 1, Screen: &temp})
				SendToServer(string(data))
			}
		}}), vecty.Text("\uF054"))
}

type Screens struct {
	vecty.Core
}

func (p *Screens) Render() vecty.ComponentOrHTML {
	return elem.Div(vecty.Markup(vecty.Class("centered")),
		elem.Div(
			vecty.Markup(vecty.Class("screens")),
			&LeftButton{},
			&ScreenView{id: 0},
			&ScreenView{id: 1},
			&ScreenView{id: 2},
			&ScreenView{id: 3},
			&RightButton{},
		),
	)
}

type PowerOnButton struct {
	vecty.Core
}

var ws js.Value

func SendToServer(s string) {
	ws.Call("send", s)
}

func OnMessage(s string) {

	var updateInfo UpdateInfo
	json.Unmarshal([]byte(s), &updateInfo)

	if updateInfo.Select!=nil && *updateInfo.Select {
		//change selection
		active = *updateInfo.Screen
		if updateInfo.Position!=nil {
			//change screen and position
			descriptors[active].selected = *updateInfo.Position
			RenderList(active)
		} else {
			if descriptors[active].navigable {
				descriptors[active].selected = 0
				//send to server change to zero
				zero := 0
				data, _ := json.Marshal(CubeInfo{Type: TYPE_CHANGE, Cube: 1, Screen: &active, State: &zero})
				SendToServer(string(data))
			}
		}
		vecty.Rerender(emulator)
	} else {
		if updateInfo.Screen != nil {
			if updateInfo.IsText {
				GetListFromNetwork(*updateInfo.Screen)
			} else {
				GetImageFromNetwork(*updateInfo.Screen)
			}
		} else {
			if updateInfo.Color != "" {
				//change color
				lightColor, _ = colors.ParseHEX(updateInfo.Color)
				vecty.Rerender(emulator)
			}
		}
	}
}

var blink js.Func
var brightness uint8
var brightnessShift int

type HelloMessage struct {
	Token *string `json:"token"`
	SN    *uint32 `json:"sn"`
	Pin   *string `json:"pin"`
}

var sn *uint32

func LoggedIn() {
	hello := HelloMessage{
		Token: token,
		SN:    sn,
		Pin:   nil,
	}
	hello_json, _ := json.Marshal(hello)
	ws.Call("addEventListener", "open", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		SendToServer(string(hello_json))
		return nil
	}))
	ws.Call("addEventListener", "close", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		ws = js.Global().Get("WebSocket").New(WSURL)
		return nil
	}))
	ws.Call("addEventListener", "message", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		arg0 := args[0].Get("data").String()
		OnMessage(arg0)
		return nil
	}))
}

func PoweringOn() {
	lightColor, _ = colors.RGBA(31, 191, 191, 1)
	//check init mode
	ws = js.Global().Get("WebSocket").New(WSURL)
	if !registration_mode {
		//register js function
		LoggedIn()
	} else {
		Register()
	}
}

func Register() {
	rand.Seed(time.Now().Unix())
	pin := rand.Intn(10000)
	DrawDigit(0, int(pin/1000))
	DrawDigit(1, (pin%1000)/100)
	DrawDigit(2, (pin%100)/10)
	DrawDigit(3, pin%10)
	DrawBorder(0, 8, 48, 16, 87)
	DrawBorder(1, 8, 110, 50, 181)
	DrawBorder(2, 8, 153, 82, 235)
	DrawBorder(3, 8, 191, 144, 245)
	brightness = 128
	brightnessShift = 2
	blink = js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		brightness = uint8(int(brightness) + brightnessShift)
		if brightness > 192 || brightness < 16 {
			brightnessShift = -brightnessShift
		}
		lightColor, _ = colors.RGB(brightness, brightness, brightness)
		vecty.Rerender(emulator)
		return nil
	})
	js.Global().Call("setInterval", blink, 20)
	pinstr := fmt.Sprintf("%04d", pin)
	ws.Call("addEventListener", "open", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		hello := HelloMessage{
			Token: nil,
			SN:    nil,
			Pin:   &pinstr,
		}
		hello_json, _ := json.Marshal(hello)
		SendToServer(string(hello_json))
		return nil
	}))
	ws.Call("addEventListener", "message", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		arg0 := args[0].Get("data").String()
		var db DeviceBound
		err := json.Unmarshal([]byte(arg0), &db)
		if err == nil {
			token = &db.Token
			sn = &db.SN
			registration_mode = false

			config := Configuration{
				Token: db.Token,
				SN:    db.SN,
			}
			configdata, _ := json.Marshal(config)
			StoreToLocalStorage("config", &configdata)
			LoggedIn()
		}
		return nil
	}))
}

func (p *PowerOnButton) Render() vecty.ComponentOrHTML {
	return elem.Section(
		elem.Anchor(vecty.Markup(
			vecty.Class("beveled-button"),
			&vecty.EventListener{
				Name: "click",
				Listener: func(event *vecty.Event) {
					powerOn = !powerOn
					if powerOn {
						PoweringOn()
					} else {
						ws.Call("close")
						lightColor = colors.FromStdColor(color.Black)
						ClearScreens()
					}
					vecty.Rerender(emulator)
				},
			},
			vecty.MarkupIf(powerOn, vecty.Class("on"))),
			vecty.Text("\uF011"),
		),
		elem.Span(),
	)
}

type DeviceBound struct {
	SN    uint32 `json:"sn"`
	Token string `json:"token"`
}

type ButtonPanel struct {
	vecty.Core
}

func (p *ButtonPanel) Render() vecty.ComponentOrHTML {
	return elem.Div(
		vecty.Markup(
			vecty.Class("centered")),
		elem.Anchor(vecty.Markup(vecty.Class("touch-button"),
			&vecty.EventListener{Name: "click", Listener: func(event *vecty.Event) {
				if descriptors[active].navigable {
					pos := descriptors[active].selected
					pos--
					if pos < 0 {
						pos = descriptors[active].count - 1
					}
					data, _ := json.Marshal(CubeInfo{Type: TYPE_CHANGE, Cube: 1, Screen: &active, State: &pos})
					SendToServer(string(data))
				}
			}},
		), vecty.Text("\uF077")),
		elem.Anchor(vecty.Markup(vecty.Class("touch-button"),
			&vecty.EventListener{Name: "click", Listener: func(event *vecty.Event) {
				var tap CubeInfo
				if descriptors[active].navigable {
					tap = CubeInfo{
						Type:   TYPE_TAP,
						Cube:   1,
						Screen: &active,
						State:  &screenLists[active][descriptors[active].selected].Number,
					}
				} else {
					tap = CubeInfo{
						Type:   TYPE_TAP,
						Cube:   0,
						Screen: &active,
						State:  nil,
					}
				}
				data, _ := json.Marshal(tap)
				SendToServer(string(data))
			},
			}), vecty.Text("\uF058")),
		elem.Anchor(vecty.Markup(vecty.Class("touch-button"),
			&vecty.EventListener{Name: "click", Listener: func(event *vecty.Event) {
				if descriptors[active].navigable {
					pos := descriptors[active].selected
					pos++
					if pos >= descriptors[active].count {
						pos = 0
					}
					data, _ := json.Marshal(CubeInfo{Type: TYPE_CHANGE, Cube: 1, Screen: &active, State: &pos})
					SendToServer(string(data))
				}
			}},
		), vecty.Text("\uF078")),
	)
}

type Emulator struct {
	vecty.Core
}

var font []byte

func (p *Emulator) Render() vecty.ComponentOrHTML {
	return elem.Body(
		&PowerOnButton{},
		&ButtonPanel{},
		&Screens{},
		&BottomLight{},
		&FlipButton{},
	)
}

type BottomLight struct {
	vecty.Core
}

func (p *BottomLight) Render() vecty.ComponentOrHTML {
	return elem.Div(
		vecty.Markup(
			vecty.Style("background", lightColor.ToHEX().String()),
			vecty.Class("bottom-light"),
		),
	)
}

var emulator *Emulator

func ClearScreen(screen int) {
	j := 0
	pixels := screens[screen].points
	for j < screenWidth*screenHeight {
		pixels[j*4] = 0
		pixels[j*4+1] = 0
		pixels[j*4+2] = 0
		pixels[j*4+3] = 255
		j++
	}
}

func ClearScreens() {
	for i := 0; i < 4; i++ {
		ClearScreen(i)
	}
}

func GetFromLocalStorage(key string) *string {
	ls := js.Global().Get("localStorage").Get(key)
	if ls.IsNull() {
		return nil
	}
	lss := ls.String()
	return &lss
}

func StoreToLocalStorage(key string, data *[]byte) {
	if data == nil {
		js.Global().Get("localStorage").Call("removeItem", key)
	} else {
		js.Global().Get("localStorage").Set(key, string(*data))
	}
}

type Configuration struct {
	Token string `json:"token"`
	SN    uint32 `json:"sn"`
}

var token *string
var registration_mode bool

func main() {
	c := GetFromLocalStorage("config")
	token = nil
	if c == nil {
		//goto registration mode
		registration_mode = true
	} else {
		var conf Configuration
		err := json.Unmarshal([]byte(*c), &conf)
		if err != nil {
			registration_mode = true
			//goto registration mode
		} else {
			token = &conf.Token
			sn = &conf.SN
			registration_mode = false
		}
	}
	//init screens
	for i := 0; i < 4; i++ {
		screen := ScreenContent{}
		screen.points = make([]byte, screenWidth*screenHeight*4)
		screens = append(screens, screen)
		descriptor := ScreenDescriptor{navigable: false, topY: 0, topLine: 0, selected: 0}
		descriptors = append(descriptors, descriptor)
		items := make([]ListItem, 0, 0)
		screenLists = append(screenLists, items)
		ClearScreen(i)
	}

	vecty.SetTitle("AirCube Emulator")
	vecty.AddStylesheet("main.css")

	powerOn = false
	lightColor = colors.FromStdColor(color.Black)

	emulator = &Emulator{}
	vecty.RenderInto("body", emulator)

	for i := 0; i < 4; i++ {
		d := js.Global().Get("document").Call("querySelector", "#canvas"+strconv.Itoa(i))
		cv, _ := canvas.NewCanvas2d(false)
		cv.Set(d, screenWidth, screenHeight)
		cv.Start(60, MakeRenderCanvas(i))
		cvs = append(cvs, cv)
	}

	go func() {
		req, _ := http.NewRequest("GET", "fonts/8x8.fnt", nil)
		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			log.Fatalln("Font isn't found")
		}
		font, _ = ioutil.ReadAll(resp.Body)

	}()

	select {}
}

type Screen struct {
	ID      int    `json:"id" example:"1"`
	Cube    int    `json:"cube" example:"1"`
	Screen  int    `json:"screen" example:"3"`
	Content string `json:"content" example:"SGVsbG8="`
	//id SERIAL, cube INT, screen INT, content BLOB
}

type ListItem struct {
	X          int     `json:"x" example:"0"`
	Y          int     `json:"y" example:"0"`
	Text       string  `json:"text" example:"Hello"`
	Number     int     `json:"number" example:"1"`
	IconWidth  *int    `json:"icon_width" example:"8"`
	IconHeight *int    `json:"icon_height" example:"8"`
	Icon       *string `json:"icon" example:""`
	Color      *string `json:"color" example:"#FFFFFF"`
}

func EncodeWindows1251(ba []uint8) []uint8 {
	enc := charmap.Windows1251.NewEncoder()
	out, _ := enc.String(string(ba))
	return []uint8(out)
}

func PrintTextLine(win1251 []byte, base_shift int, screen int, x int, y int, r byte, g byte, b byte) {

	for c := 0; c < len(win1251); c++ {
		var ch = win1251[c]
		pos := int(ch) * 8
		for cy := 0; cy < 8; cy++ {
			if y+cy >= screenHeight || y+cy < base_shift {
				continue
			}
			row := font[pos+cy]
			for cx := 7; cx >= 0; cx-- {
				if row%2 != 0 {
					SetPixel(screen, x+cx, y+cy, r, g, b)
				}
				row = row >> 1
			}
		}
		x += 8
		if x >= screenWidth {
			x = 0
			y += 8
		}
	}
}

func RenderList(screen int) {
	ClearScreen(screen)
	list := screenLists[screen]
	descriptors[screen].count = len(list)

	top_shift := descriptors[screen].topY
	top_line := descriptors[screen].topLine
	selt := descriptors[screen].selected

	if selt>=len(list) {
		return
	}

	sely := list[selt].Y - top_shift
	baseShift := 0
	if descriptors[screen].title != nil {
		baseShift = 24
		win1251 := EncodeWindows1251([]uint8(*descriptors[screen].title))
		PrintTextLine(win1251, 0, screen, 8, 8, 255, 255, 255)
		for x := 0; x <= screenWidth; x++ {
			SetPixel(screen, x, 20, 255, 255, 255)
		}
	}
	//scroll up
	if sely+8 >= screenHeight-baseShift {
		shift := 1
		delta := 0
		for {
			//todo: problem!!!
			delta = list[top_line+shift].Y - list[top_line].Y
			if sely+8-delta < screenHeight-baseShift {
				break
			}
			shift++
		}
		descriptors[screen].topLine += shift
		descriptors[screen].topY += delta
		top_shift = descriptors[screen].topY
	}
	if sely < 0 {
		shift := 1
		delta := 0
		//detect bottom line
		line := top_line
		for {
			if list[line].Y+8 >= screenHeight-baseShift {
				break
			}
			line++
			if line >= len(list) {
				line--
				break
			}
		}
		//line - id последней видимой целиком строки
		for {
			delta = list[line].Y - list[line-shift].Y

			if sely+delta >= 0 {
				break
			}
			shift++
		}
		descriptors[screen].topLine -= shift
		if descriptors[screen].topLine < 0 {
			descriptors[screen].topLine = 0
		}
		descriptors[screen].topY -= delta
		top_shift = descriptors[screen].topY
	}

	for i := 0; i < len(list); i++ {
		x := list[i].X
		y := list[i].Y
		y -= top_shift
		y += baseShift
		text := []byte(list[i].Text)

		color := list[i].Color
		var r byte
		var g byte
		var b byte
		if color == nil {
			r = 255
			g = 255
			b = 255
		} else {
			c, _ := colors.ParseHEX(*color)
			rgb := c.ToRGB()
			r = rgb.R
			g = rgb.G
			b = rgb.B
		}

		win1251 := EncodeWindows1251(text)
		line_height := 8
		xdelta := 0
		yshift := 0
		if list[i].Icon != nil {
			//draw icon
			icon, _ := base64.StdEncoding.DecodeString(*list[i].Icon)
			for iy := 0; iy < *list[i].IconHeight; iy++ {
				for ix := 0; ix < *list[i].IconWidth; ix++ {
					pos := iy*(*list[i].IconWidth) + ix
					screen_pos := (y+iy)*screenWidth + (x + ix)
					if y+iy < screenHeight && y+iy >= baseShift && x+ix >= 0 && x+ix < screenWidth {
						SetPoint(screen, icon, pos, screen_pos)
					}
				}
			}
			xdelta = *list[i].IconWidth
			x = x + *list[i].IconWidth + 4
			if *list[i].IconHeight > line_height {
				line_height = *list[i].IconHeight
				yshift = (line_height - 8) / 2
			}
		}
		PrintTextLine(win1251, baseShift, screen, x, y+yshift, r, g, b)

		if active == screen && descriptors[screen].selected == i && descriptors[screen].navigable {
			left := list[i].X - 2
			right := list[i].X + len(win1251)*8 + xdelta + 4
			top := list[i].Y - top_shift - 4 + baseShift
			bottom := list[i].Y - top_shift + line_height + 2 + baseShift
			if top < screenHeight && top >= baseShift {
				for x := left; x <= right; x++ {
					SetPixel(screen, x, top, 255, 255, 255)
				}
			}
			if bottom < screenHeight && bottom >= baseShift {
				for x := left; x <= right; x++ {
					SetPixel(screen, x, bottom, 255, 255, 255)
				}
			}
			for y := top; y <= bottom; y++ {
				if y < screenHeight && y >= baseShift {
					SetPixel(screen, left, y, 255, 255, 255)
				}
			}
			for y := top; y <= bottom; y++ {
				if y < screenHeight && y >= baseShift {
					SetPixel(screen, right, y, 255, 255, 255)
				}
			}
		}
	}
}

func SetPoint(screen int, img []byte, i int, pos int) {
	var point uint16
	point = uint16(img[i*2+1])
	point = point << 8
	point = point + uint16(img[i*2])
	b := point % 32
	g := (point >> 5) % 64
	r := (point >> 11) % 32
	if flipped {
		pos = screenHeight*screenWidth - 1 - pos
	}
	screens[screen].points[pos*4] = byte(r << 3)
	screens[screen].points[pos*4+1] = byte(g << 2)
	screens[screen].points[pos*4+2] = byte(b << 3)
	screens[screen].points[pos*4+3] = 255
}

func SetScreen(screen int, img []byte) {
	if powerOn {
		i := 0
		for x := 0; x < screenWidth; x++ {
			for y := 0; y < screenHeight; y++ {
				SetPoint(screen, img, i, (screenWidth-1-x)+screenWidth*y)
				i++
			}
		}
		//for i := 0; i < len(img)/2; i++ {
		//	SetPoint(screen, img, i, i)
		//}
	}
}

func GetImageFromNetwork(screen int) {
	go func() {
		req, _ := http.NewRequest("GET", URLPrefix+"/screen/"+strconv.Itoa(screen), nil)
		req.Header.Set("Authorization", "bearer "+*token)
		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil || resp.StatusCode != 200 {
			return
		}
		content, _ := ioutil.ReadAll(resp.Body)
		//rotate!!!
		SetScreen(screen, content)
	}()
}

func GetListFromNetwork(screen int) {
	go func() {
		client := &http.Client{}
		req2, err2 := http.NewRequest("GET", URLPrefix+"/list/"+strconv.Itoa(screen), nil)
		req2.Header.Set("Authorization", "bearer "+*token)
		resp2, _ := client.Do(req2)
		if err2 != nil || resp2.StatusCode == 404 {
			return
		}
		var result ListDescriptor
		content, _ := ioutil.ReadAll(resp2.Body)
		decoder := json.NewDecoder(bytes.NewReader(content))
		decoder.Decode(&result)
		screenLists[screen] = result.Items
		descriptors[screen].title = result.Title
		descriptors[screen].navigable = result.Navigable
		descriptors[screen].topY = 0
		descriptors[screen].list = true
		UpdateScreen(screen)
	}()
}

type ListDescriptor struct {
	Title     *string    `json:"title"`
	Navigable bool       `json:"navigable"`
	Items     []ListItem `json:"items"`
}

func UpdateScreens() {
	if powerOn {
		for i := 0; i < 4; i++ {
			UpdateScreen(i)
		}
	}
}

func UpdateScreen(screen int) {
	if descriptors[screen].list {
		RenderList(screen)
	} else {
		GetImageFromNetwork(screen)
	}
}

func SetPixel(screen int, x int, y int, r byte, g byte, b byte) {

	sh := y*screenWidth + x
	if flipped {
		sh = screenHeight*screenWidth - 1 - sh
	}

	screens[screen].points[sh*4] = r
	screens[screen].points[sh*4+1] = g
	screens[screen].points[sh*4+2] = b
	screens[screen].points[sh*4+3] = 255
}

func DrawBorder(screen int, width int, r byte, g byte, b byte) {
	for y := 0; y < width; y++ {
		for x := 0; x < screenWidth; x++ {
			SetPixel(screen, x, y, r, g, b)
		}
	}
	for y := screenHeight - width; y < screenHeight; y++ {
		for x := 0; x < screenWidth; x++ {
			SetPixel(screen, x, y, r, g, b)
		}
	}
	for x := 0; x < width; x++ {
		for y := 0; y < screenHeight; y++ {
			SetPixel(screen, x, y, r, g, b)
		}
	}
	for x := screenWidth - width; x < screenWidth; x++ {
		for y := 0; y < screenHeight; y++ {
			SetPixel(screen, x, y, r, g, b)
		}
	}
}

func DrawDigit(screen int, digit int) {
	var digits []byte
	digits = make([]byte, 128, 128)
	digits = []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xF8, 0x07, 0x00,
		0x00, 0xFE, 0x3F, 0x00, 0x00, 0xFF, 0xFF, 0x00, 0xC0, 0x5F, 0xFE, 0x01,
		0xC0, 0x0F, 0xF0, 0x01, 0xE0, 0x03, 0xE0, 0x03, 0xF0, 0x03, 0xE0, 0x03,
		0xF0, 0x03, 0xE0, 0x07, 0xF0, 0x01, 0xC0, 0x07, 0xE0, 0x01, 0xC0, 0x07,
		0xF0, 0x03, 0xC0, 0x07, 0xF0, 0x01, 0xC0, 0x07, 0xF0, 0x03, 0xC0, 0x07,
		0xE0, 0x03, 0xE0, 0x07, 0xE0, 0x03, 0xE0, 0x03, 0xE0, 0x07, 0xF0, 0x03,
		0xC0, 0x0F, 0xF8, 0x01, 0x80, 0xFF, 0xFF, 0x00, 0x00, 0xFF, 0x7F, 0x00,
		0x00, 0xFC, 0x1F, 0x00, 0x00, 0x60, 0x02, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		//1
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xF8, 0x03, 0x00,
		0x00, 0xFC, 0x03, 0x00, 0x80, 0xFF, 0x03, 0x00, 0xE0, 0xFF, 0x03, 0x00,
		0xE0, 0xE7, 0x07, 0x00, 0xA0, 0xE1, 0x03, 0x00, 0x00, 0xE0, 0x03, 0x00,
		0x00, 0xE0, 0x03, 0x00, 0x00, 0xE0, 0x03, 0x00, 0x00, 0xE0, 0x07, 0x00,
		0x00, 0xE0, 0x03, 0x00, 0x00, 0xE0, 0x03, 0x00, 0x00, 0xE0, 0x03, 0x00,
		0x00, 0xE0, 0x07, 0x00, 0x00, 0xE0, 0x03, 0x00, 0x00, 0xE0, 0x03, 0x00,
		0x00, 0xE0, 0x07, 0x00, 0xE0, 0xFF, 0xFF, 0x03, 0xF0, 0xFF, 0xFF, 0x07,
		0xE0, 0xFF, 0xFF, 0x03, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		//2
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xF8, 0x07, 0x00,
		0x00, 0xFF, 0x3F, 0x00, 0xC0, 0xFF, 0xFF, 0x00, 0xE0, 0x2F, 0xFE, 0x01,
		0xF0, 0x03, 0xF0, 0x03, 0xF0, 0x01, 0xE0, 0x03, 0xE0, 0x00, 0xF0, 0x03,
		0x00, 0x00, 0xF0, 0x03, 0x00, 0x00, 0xFC, 0x01, 0x00, 0x00, 0xFE, 0x00,
		0x00, 0x80, 0x7F, 0x00, 0x00, 0xE0, 0x1F, 0x00, 0x00, 0xF8, 0x0B, 0x00,
		0x00, 0xFE, 0x01, 0x00, 0x80, 0xFF, 0x00, 0x00, 0xE0, 0x3F, 0xC0, 0x01,
		0xF8, 0x0F, 0xE0, 0x03, 0xF8, 0xFF, 0xFF, 0x03, 0xF8, 0xFF, 0xFF, 0x03,
		0xF8, 0xFF, 0xFF, 0x03, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		//3
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xF8, 0x0F, 0x00,
		0x80, 0xFF, 0x7F, 0x00, 0xC0, 0xFF, 0xFF, 0x01, 0xE0, 0x2F, 0xFD, 0x01,
		0xE0, 0x01, 0xF0, 0x03, 0x00, 0x00, 0xE0, 0x03, 0x00, 0x00, 0xE0, 0x03,
		0x00, 0x00, 0xF8, 0x01, 0x00, 0xF0, 0xFF, 0x00, 0x00, 0xF0, 0x7F, 0x00,
		0x00, 0xF0, 0xFF, 0x00, 0x00, 0x00, 0xFC, 0x03, 0x00, 0x00, 0xF0, 0x03,
		0x00, 0x00, 0xC0, 0x07, 0x00, 0x00, 0xC0, 0x0F, 0x00, 0x00, 0xE0, 0x07,
		0x60, 0x00, 0xF0, 0x07, 0xF0, 0xDF, 0xFF, 0x03, 0xF0, 0xFF, 0xFF, 0x01,
		0xE0, 0xFF, 0x3F, 0x00, 0x00, 0x64, 0x02, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		//4
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x04, 0x00,
		0x00, 0x80, 0xFF, 0x00, 0x00, 0x80, 0x7F, 0x00, 0x00, 0xE0, 0x7F, 0x00,
		0x00, 0xF0, 0xFF, 0x00, 0x00, 0xF8, 0x7F, 0x00, 0x00, 0xF8, 0x7C, 0x00,
		0x00, 0xFE, 0xFC, 0x00, 0x00, 0x3F, 0x7C, 0x00, 0x00, 0x3F, 0x7C, 0x00,
		0xC0, 0x0F, 0x7C, 0x00, 0xE0, 0x07, 0x7C, 0x00, 0xE0, 0x5F, 0xFE, 0x00,
		0xF8, 0xFF, 0xFF, 0x03, 0xF0, 0xFF, 0xFF, 0x03, 0xD0, 0xDB, 0xFF, 0x01,
		0x00, 0x00, 0x7C, 0x00, 0x00, 0xE0, 0xFF, 0x03, 0x00, 0xF0, 0xFF, 0x03,
		0x00, 0xE0, 0xFF, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		//5
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x41, 0x44, 0x00,
		0xC0, 0xFF, 0xFF, 0x01, 0xC0, 0xFF, 0xFF, 0x01, 0xE0, 0xFF, 0xFF, 0x00,
		0xC0, 0x0F, 0x00, 0x00, 0xC0, 0x0F, 0x00, 0x00, 0xC0, 0x07, 0x00, 0x00,
		0xC0, 0xFF, 0x1F, 0x00, 0xC0, 0xFF, 0xFF, 0x00, 0xC0, 0xFF, 0xFF, 0x01,
		0x80, 0x0F, 0xF8, 0x03, 0x00, 0x00, 0xF0, 0x07, 0x00, 0x00, 0xC0, 0x07,
		0x00, 0x00, 0xC0, 0x0F, 0x00, 0x00, 0xC0, 0x07, 0x00, 0x00, 0xE0, 0x07,
		0xF0, 0x01, 0xF0, 0x07, 0xF0, 0xDF, 0xFF, 0x01, 0xF0, 0xFF, 0xFF, 0x00,
		0xC0, 0xFF, 0x3F, 0x00, 0x00, 0x68, 0x02, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		//6
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xFE, 0x03,
		0x00, 0xC0, 0xFF, 0x07, 0x00, 0xF0, 0xFF, 0x0F, 0x00, 0xFC, 0x9F, 0x03,
		0x00, 0xFF, 0x00, 0x00, 0x00, 0x7F, 0x00, 0x00, 0x80, 0x1F, 0x00, 0x00,
		0xC0, 0x0F, 0x04, 0x00, 0xC0, 0xE7, 0x3F, 0x00, 0xC0, 0xFF, 0xFF, 0x00,
		0xE0, 0xFF, 0xFF, 0x03, 0xE0, 0x7F, 0xF0, 0x07, 0xC0, 0x0F, 0xC0, 0x07,
		0xC0, 0x0F, 0xC0, 0x0F, 0xC0, 0x0F, 0x80, 0x0F, 0x80, 0x0F, 0xC0, 0x0F,
		0x80, 0x3F, 0xE0, 0x07, 0x00, 0xFF, 0xFB, 0x03, 0x00, 0xFE, 0xFF, 0x01,
		0x00, 0xF8, 0x7F, 0x00, 0x00, 0x40, 0x05, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		//7
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x80, 0x21, 0x44, 0x02,
		0xF8, 0xFF, 0xFF, 0x03, 0xF0, 0xFF, 0xFF, 0x03, 0xF0, 0xF7, 0xFF, 0x03,
		0xF0, 0x01, 0xF0, 0x03, 0x60, 0x00, 0xF0, 0x01, 0x00, 0x00, 0xF8, 0x01,
		0x00, 0x00, 0xF8, 0x00, 0x00, 0x00, 0xFC, 0x00, 0x00, 0x00, 0x7E, 0x00,
		0x00, 0x00, 0x7E, 0x00, 0x00, 0x00, 0x3F, 0x00, 0x00, 0x00, 0x1F, 0x00,
		0x00, 0x80, 0x1F, 0x00, 0x00, 0x80, 0x0F, 0x00, 0x00, 0xC0, 0x0F, 0x00,
		0x00, 0xC0, 0x07, 0x00, 0x00, 0xE0, 0x07, 0x00, 0x00, 0xE0, 0x03, 0x00,
		0x00, 0xE0, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		//8
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xF8, 0x07, 0x00,
		0x00, 0xFF, 0x3F, 0x00, 0x80, 0xFF, 0xFF, 0x00, 0xC0, 0x9F, 0xFC, 0x01,
		0xE0, 0x07, 0xF0, 0x03, 0xE0, 0x03, 0xE0, 0x03, 0xE0, 0x03, 0xE0, 0x03,
		0xE0, 0x07, 0xF0, 0x03, 0xC0, 0x7F, 0xFD, 0x00, 0x00, 0xFF, 0x7F, 0x00,
		0x00, 0xFF, 0x7F, 0x00, 0xC0, 0xBF, 0xFF, 0x01, 0xE0, 0x0F, 0xF8, 0x03,
		0xE0, 0x03, 0xE0, 0x03, 0xF0, 0x03, 0xE0, 0x07, 0xF0, 0x03, 0xE0, 0x07,
		0xE0, 0x07, 0xF0, 0x03, 0xC0, 0xFF, 0xFF, 0x03, 0xC0, 0xFF, 0xFF, 0x00,
		0x00, 0xFE, 0x3F, 0x00, 0x00, 0x60, 0x02, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		//9
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xF0, 0x1F, 0x00,
		0x00, 0xFE, 0x7F, 0x00, 0x00, 0xFF, 0xFF, 0x01, 0xC0, 0x3F, 0xFA, 0x03,
		0xC0, 0x0F, 0xF0, 0x03, 0xC0, 0x07, 0xE0, 0x07, 0xC0, 0x07, 0xC0, 0x0F,
		0xC0, 0x07, 0xC0, 0x0F, 0xC0, 0x0F, 0xF0, 0x07, 0x80, 0x3F, 0xFE, 0x0F,
		0x00, 0xFF, 0xFF, 0x0F, 0x00, 0xFE, 0xDF, 0x0F, 0x00, 0xF0, 0xC7, 0x07,
		0x00, 0x00, 0xE0, 0x07, 0x00, 0x00, 0xF0, 0x03, 0x00, 0x00, 0xFC, 0x01,
		0x00, 0x00, 0xFF, 0x00, 0xC0, 0xFF, 0x7F, 0x00, 0xC0, 0xFF, 0x1F, 0x00,
		0xC0, 0xFF, 0x07, 0x00, 0x00, 0x64, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	}

	ClearScreen(screen)
	digitSize := 4
	pos := digit * 32 * 4
	y := 0
	x := 16
	for cy := 0; cy < 32; cy++ {
		for shift := 3; shift >= 0; shift-- {
			row := int(digits[pos+cy*4+shift])
			for cx := 7; cx >= 0; cx-- {
				if row%2 != 0 {
					for dy := 0; dy < digitSize; dy++ {
						for dx := 0; dx < digitSize; dx++ {
							SetPixel(screen, x+((7-cx)+shift*8)*digitSize+dx, y+cy*digitSize+dy, 255, 255, 255)
						}
					}
				}
				row = row >> 1
			}
		}
	}
}

func MakeRenderCanvas(screen int) func(*draw2dimg.GraphicContext) bool {
	return func(gc *draw2dimg.GraphicContext) bool {
		gc.SetFillColor(color.RGBA{0xff, 0x00, 0xff, 0xff})
		gc.SetStrokeColor(color.RGBA{0xFF, 0x00, 0x00, 0xFF})
		gc.BeginPath()
		//gc.MoveTo(0, 0)
		//gc.LineTo(float64(width), float64(height))
		//gc.MoveTo(float64(width), 0)
		//gc.LineTo(0, float64(height))
		img := &image.NRGBA{Pix: screens[screen].points, Rect: image.Rect(0, 0, screenWidth, screenHeight), Stride: screenWidth * 4}
		gc.DrawImage(img)
		gc.Stroke()
		gc.Close()
		return true
	}
}

type UpdateInfo struct {
	Screen   *int   `json:"screen"`
	IsText   bool   `json:"is_text"`
	Color    string `json:"color"`
	Position *int   `json:"position"`
	Select   *bool  `json:"select"`
}

type CubeInfo struct {
	Type   int  `json:"type"`
	Cube   int  `json:"cube"`
	Screen *int `json:"screen"`
	State  *int `json:"state"`
}
