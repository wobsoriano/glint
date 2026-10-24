// Package settings reads and writes Glint's settings.json and presets.json in the data
// directory. The JSON is the contract the Rust build wrote: the same field names, enum
// spellings and defaults for missing fields, with unknown fields ignored.
package settings

import (
	"encoding/json"
)

type AfterShot struct {
	Preview bool `json:"preview"`
	Copy    bool `json:"copy"`
	Save    bool `json:"save"`
	Edit    bool `json:"edit"`
	Pin     bool `json:"pin"`
}

type AfterRecording struct {
	Preview bool `json:"preview"`
	Copy    bool `json:"copy"`
	Save    bool `json:"save"`
	Studio  bool `json:"studio"`
}

type RecordTarget string

const (
	RecordDisplay RecordTarget = "Display"
	RecordWindow  RecordTarget = "Window"
	RecordArea    RecordTarget = "Area"
)

func (v *RecordTarget) UnmarshalJSON(b []byte) (err error) {
	*v, err = parseEnum(b, []RecordTarget{RecordDisplay, RecordWindow, RecordArea})
	return err
}

// Area is x, y, width, height in physical pixels of the virtual desktop.
type Area [4]int32

func (a *Area) UnmarshalJSON(b []byte) error {
	xs, err := fixedArray[int32](b, 4)
	if err != nil {
		return err
	}
	*a = Area(xs)
	return nil
}

type RecordPrefs struct {
	Target        RecordTarget `json:"target"`
	Area          *Area        `json:"area"`
	Camera        *string      `json:"camera"`
	Mic           *string      `json:"mic"`
	SystemAudio   bool         `json:"system_audio"`
	Countdown     uint32       `json:"countdown"`
	Teleprompter  bool         `json:"teleprompter"`
	Script        string       `json:"script"`
	PrompterLines uint32       `json:"prompter_lines"`
}

type Side string

const (
	SideLeft  Side = "Left"
	SideRight Side = "Right"
)

func (v *Side) UnmarshalJSON(b []byte) (err error) {
	*v, err = parseEnum(b, []Side{SideLeft, SideRight})
	return err
}

type OverlayPrefs struct {
	Side             Side       `json:"side"`
	AutoCloseSeconds uint32     `json:"auto_close_seconds"`
	DismissAfterDrag bool       `json:"dismiss_after_drag"`
	Layout           CardLayout `json:"layout"`
}

type ExportFormat string

const (
	FormatPNG  ExportFormat = "Png"
	FormatJPEG ExportFormat = "Jpeg"
)

func (v *ExportFormat) UnmarshalJSON(b []byte) (err error) {
	*v, err = parseEnum(b, []ExportFormat{FormatPNG, FormatJPEG})
	return err
}

func (v ExportFormat) Extension() string {
	if v == FormatJPEG {
		return "jpg"
	}
	return "png"
}

type LibraryView string

const (
	ViewGrid LibraryView = "Grid"
	ViewList LibraryView = "List"
)

func (v *LibraryView) UnmarshalJSON(b []byte) (err error) {
	*v, err = parseEnum(b, []LibraryView{ViewGrid, ViewList})
	return err
}

// SortKey orders the Library: Captured and Modified put the newest first, Name is A to Z.
type SortKey string

const (
	SortCaptured SortKey = "Captured"
	SortName     SortKey = "Name"
	SortModified SortKey = "Modified"
)

func (v *SortKey) UnmarshalJSON(b []byte) (err error) {
	*v, err = parseEnum(b, []SortKey{SortCaptured, SortName, SortModified})
	return err
}

type LibraryPrefs struct {
	View      LibraryView `json:"view"`
	Sort      SortKey     `json:"sort"`
	Inspector bool        `json:"inspector"`
}

// RedactStyle is how a redaction hides what is under it.
type RedactStyle string

const (
	RedactBlur     RedactStyle = "Blur"
	RedactPixelate RedactStyle = "Pixelate"
	RedactSolid    RedactStyle = "Solid"
)

var RedactStyles = []RedactStyle{RedactBlur, RedactPixelate, RedactSolid}

func (v *RedactStyle) UnmarshalJSON(b []byte) (err error) {
	*v, err = parseEnum(b, RedactStyles)
	return err
}

// RedactRules picks which kinds of sensitive text automatic redaction looks for.
type RedactRules struct {
	Emails         bool `json:"emails"`
	Phones         bool `json:"phones"`
	CreditCards    bool `json:"credit_cards"`
	Secrets        bool `json:"secrets"`
	IPAddresses    bool `json:"ip_addresses"`
	IDs            bool `json:"ids"`
	URLsWithTokens bool `json:"urls_with_tokens"`
}

type HotkeyAction int

const (
	HotkeyFullscreen HotkeyAction = iota
	HotkeyWindow
	HotkeyArea
	HotkeyPreviousArea
	HotkeyText
	HotkeyRecording
	HotkeyLibrary
)

// HotkeyActions is the order the hotkey preferences list them in.
var HotkeyActions = []HotkeyAction{
	HotkeyArea, HotkeyFullscreen, HotkeyWindow, HotkeyPreviousArea, HotkeyText, HotkeyRecording, HotkeyLibrary,
}

func (a HotkeyAction) Label() string {
	return [...]string{
		HotkeyFullscreen:   "Capture full screen",
		HotkeyWindow:       "Capture window",
		HotkeyArea:         "Capture area",
		HotkeyPreviousArea: "Capture previous area",
		HotkeyText:         "Capture text",
		HotkeyRecording:    "Record screen",
		HotkeyLibrary:      "Open Library",
	}[a]
}

// Hotkeys holds accelerators such as "Ctrl+Shift+4"; an empty string means none.
type Hotkeys struct {
	Area         string `json:"area"`
	Fullscreen   string `json:"fullscreen"`
	Window       string `json:"window"`
	PreviousArea string `json:"previous_area"`
	Text         string `json:"text"`
	Recording    string `json:"recording"`
	Library      string `json:"library"`
}

// Binding returns the accelerator field for a, to read or assign.
func (h *Hotkeys) Binding(a HotkeyAction) *string {
	return [...]*string{
		HotkeyFullscreen:   &h.Fullscreen,
		HotkeyWindow:       &h.Window,
		HotkeyArea:         &h.Area,
		HotkeyPreviousArea: &h.PreviousArea,
		HotkeyText:         &h.Text,
		HotkeyRecording:    &h.Recording,
		HotkeyLibrary:      &h.Library,
	}[a]
}

type Settings struct {
	// Style is the default screenshot look, kept as the JSON the file holds until
	// internal/render's Style replaces it.
	Style             json.RawMessage `json:"style"`
	LaunchAtLogin     bool            `json:"launch_at_login"`
	IncludeOwnWindows bool            `json:"include_own_windows"`
	TimerSeconds      uint32          `json:"timer_seconds"`
	Format            ExportFormat    `json:"format"`
	JPEGQuality       uint8           `json:"jpeg_quality"`
	SaveDir           string          `json:"save_dir"`
	SaveWithoutDialog bool            `json:"save_without_dialog"`
	WindowShadow      bool            `json:"window_shadow"`
	ShowLoupe         bool            `json:"show_loupe"`
	AfterShot         AfterShot       `json:"after_shot"`
	AfterRecording    AfterRecording  `json:"after_recording"`
	Record            RecordPrefs     `json:"record"`
	Overlay           OverlayPrefs    `json:"overlay"`
	Hotkeys           Hotkeys         `json:"hotkeys"`
	AutoRedact        bool            `json:"auto_redact"`
	RedactStyle       RedactStyle     `json:"redact_style"`
	RedactRules       RedactRules     `json:"redact_rules"`
	LastArea          *Area           `json:"last_area"`
	Library           LibraryPrefs    `json:"library"`
}

// defaultStyle is what the Rust build writes for Style::default().
const defaultStyle = `{
    "background": {
      "Gradient": {
        "stops": [
          1667691007,
          2824206335,
          3964180991
        ],
        "angle": 135.0
      }
    },
    "padding": 64.0,
    "inset": 0.0,
    "auto_balance": true,
    "radius": 12.0,
    "shadow": 40.0,
    "shadow_opacity": 0.45,
    "aspect": null,
    "align": 4,
    "chrome": "None",
    "chrome_title": true
  }`

func DefaultRedactRules() RedactRules {
	return RedactRules{Emails: true, Phones: true, CreditCards: true, Secrets: true, IPAddresses: true, IDs: true, URLsWithTokens: true}
}

func Default() Settings {
	return Settings{
		Style:             json.RawMessage(defaultStyle),
		TimerSeconds:      5,
		Format:            FormatPNG,
		JPEGQuality:       85,
		SaveDir:           DefaultSaveDir(),
		SaveWithoutDialog: true,
		WindowShadow:      true,
		ShowLoupe:         true,
		AfterShot:         AfterShot{Preview: true, Copy: true},
		AfterRecording:    AfterRecording{Preview: true},
		Record: RecordPrefs{
			Target:        RecordDisplay,
			SystemAudio:   true,
			Countdown:     3,
			PrompterLines: 3,
		},
		Overlay: OverlayPrefs{Side: SideLeft, DismissAfterDrag: true, Layout: DefaultCardLayout()},
		Hotkeys: Hotkeys{
			Area:         "Ctrl+Shift+4",
			Fullscreen:   "Ctrl+Shift+3",
			Window:       "Ctrl+Shift+5",
			PreviousArea: "Ctrl+Shift+6",
			Text:         "Ctrl+Shift+2",
			Recording:    "Ctrl+Shift+7",
			Library:      "Ctrl+Shift+L",
		},
		RedactStyle: RedactBlur,
		RedactRules: DefaultRedactRules(),
		Library:     LibraryPrefs{View: ViewGrid, Sort: SortCaptured, Inspector: true},
	}
}
