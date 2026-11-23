package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/egoist/mygo"
)

var uiThemeSelftestMatrix = []struct {
	id        string
	rowHeight int
}{
	{"harbor", 88},
}

func uiThemeSelftestCheck(theme, mode string) string {
	return "interface theme " + theme + " " + mode + " through actual UI"
}

const uiThemeOpenSettings = `(() => {
  const button=Array.from(document.querySelectorAll('.sidebar button')).find(button =>
    /^(设置|Settings)$/.test((button.getAttribute('aria-label')||button.textContent).trim()));
  if(!button||button.disabled)return false;
  button.click();return true;
})()`

const uiThemeOpenInbox = `(() => {
  const button=Array.from(document.querySelectorAll('.sidebar button')).find(button =>
    /^(收件箱|Inbox)(\s|\d|$)/.test((button.getAttribute('aria-label')||button.textContent).trim()));
  if(!button||button.disabled)return false;
  button.click();return true;
})()`

const uiThemeSelectMessage = `(() => {
  const row=Array.from(document.querySelectorAll('.message-list-row')).find(row =>
    row.querySelector('.message-row-subject')?.textContent.trim()==='GoMail selftest');
  if(!row||!row.getClientRects().length)return false;
  row.click();return true;
})()`

func uiThemeRestoreSettingsTabScript(settingsTab string) string {
	return fmt.Sprintf(`(() => {
  const requested=document.getElementById('settings-tab-'+%s);
  const fallback=document.getElementById('settings-tab-accounts');
  const target=requested&&!requested.disabled?requested:fallback;
  if(!target||target.disabled)return false;
  target.click();return true;
})()`, strconv.Quote(settingsTab))
}

func uiThemeSettingsTabRestoredScript(settingsTab string) string {
	return fmt.Sprintf(`(() => {
  const namespace=localStorage.getItem('gomail:profile-storage-migrated');
  return Boolean(namespace)&&sessionStorage.getItem('gomail:profile:'+namespace+':gomail-settings-tab')===%s;
})()`, strconv.Quote(settingsTab))
}

func uiThemeGlassDarkSidebarScript() string {
	return `(() => {
  const account=document.querySelector('.sidebar-account-row[data-selected="true"]');
  const folder=document.querySelector('.sidebar-row[data-selected="true"]');
  if(!account||!folder)return false;
  const channels=(color)=>color.match(/[\d.]+/g)?.map(Number)||[];
  const accountRGB=channels(getComputedStyle(account).backgroundColor);
  const folderRGB=channels(getComputedStyle(folder).backgroundColor);
  const accountIsDark=accountRGB.length>=3&&Math.max(...accountRGB.slice(0,3))<100;
  const folderIsDark=folderRGB.length>=3&&Math.max(...folderRGB.slice(0,3))<100;
  return accountIsDark&&folderIsDark;
})()`
}

func uiThemeOpenAppearance(ctx context.Context, window *mygo.Window) bool {
	return waitSelftestPage(ctx, window, uiThemeOpenSettings, 5*time.Second) &&
		waitSelftestPage(ctx, window, `(() => {
  const tab=document.getElementById('settings-tab-appearance');
  if(!tab||tab.disabled)return false;
  tab.click();return true;
})()`, 5*time.Second) &&
		waitSelftestPage(ctx, window, `document.querySelectorAll('[data-app-theme-option]').length===1`, 5*time.Second)
}

func uiThemeChooseScript(theme, mode, messageTheme string) string {
	return fmt.Sprintf(`(() => {
  const theme=document.querySelector('[data-app-theme-option='+CSS.escape(%s)+']');
  const labels={light:['Light','浅色'],dark:['Dark','深色'],system:['System','跟随系统']};
  const mode=Array.from(document.querySelectorAll('.appearance-settings button')).find(button =>
    labels[%s]?.includes(button.firstElementChild?.textContent.trim()));
  const message=document.querySelector('[data-message-theme-option='+CSS.escape(%s)+']');
  if(!theme||!mode||!message)return false;
  theme.click();mode.click();message.click();return true;
})()`, strconv.Quote(theme), strconv.Quote(mode), strconv.Quote(messageTheme))
}

func uiThemeRenderedScript(theme, mode string, rowHeight int) string {
	return fmt.Sprintf(`(() => {
  const root=document.documentElement,css=getComputedStyle(root);
  const rows=Array.from(document.querySelectorAll('.message-list-row')).filter(row=>row.getClientRects().length);
  const detail=document.querySelector('.message-detail-container[data-message-theme="app"]');
  const title=detail?.querySelector('.message-detail-title');
  const sheet=detail?.querySelector('.app-reader-sheet');
  const scroll=detail?.querySelector('.app-reader-scroll');
  const namespace=localStorage.getItem('gomail:profile-storage-migrated');
  const stored=namespace?'gomail:profile:'+namespace+':gomail-app-theme':null;
  if(root.dataset.uiTheme!==%s||root.dataset.theme!==%s||root.hasAttribute('data-theme-switching')||!stored||localStorage.getItem(stored)!==%s)return false;
  if(parseFloat(css.getPropertyValue('--ui-mail-row-height'))!==%d||!rows.length||!detail||!title||!sheet||!scroll)return false;
  if(document.body.innerText.includes('Something went wrong')||document.body.innerText.includes('发生了一些错误'))return false;
  if(document.documentElement.scrollWidth>innerWidth+1||scroll.scrollWidth>scroll.clientWidth+2)return false;
  const box=detail.getBoundingClientRect(),row=rows[0].getBoundingClientRect();
  if(box.width<200||box.right>innerWidth+2||box.left<0||row.height<%d-12||row.height>%d+80)return false;
  const size=parseFloat(getComputedStyle(title).fontSize);
  const expected={harbor:24};
  if(Math.abs(size-expected[%s])>1)return false;
  if(!getComputedStyle(title).fontFamily)return false;
  return true;
})()`, strconv.Quote(theme), strconv.Quote(mode), strconv.Quote(theme), rowHeight,
		rowHeight, rowHeight, strconv.Quote(theme))
}

func uiThemeNarrowReaderScript(width int) string {
	return fmt.Sprintf(`(() => {
  const list=document.querySelector('.mail-list-panel'),sidebar=document.querySelector('.sidebar');
  const detail=document.querySelector('.message-detail-container[data-message-theme="app"]');
  const back=detail?.querySelector('.message-detail-back');
  const scroll=detail?.querySelector('.app-reader-scroll');
  if(innerWidth!==%d||!list||!sidebar||!detail||!back||!scroll)return false;
  if(getComputedStyle(list).display!=='none'||getComputedStyle(sidebar).display!=='none')return false;
  const box=detail.getBoundingClientRect();
  // The desktop shell reserves a 13px scrollbar gutter on narrow windows.
  return back.getClientRects().length>0&&box.left>=-1&&box.width>=innerWidth-24&&box.right<=innerWidth+1&&
    scroll.scrollWidth<=scroll.clientWidth+1&&document.documentElement.scrollWidth<=innerWidth+1;
})()`, width)
}

func uiThemeNarrowPickerScript(width int) string {
	return fmt.Sprintf(`(() => {
  const picker=document.querySelector('.app-theme-picker'),panel=document.querySelector('.settings-panel-scroll');
  if(innerWidth!==%d||!picker||!panel)return false;
  panel.scrollTop=0;
  const box=picker.getBoundingClientRect();
  return picker.querySelectorAll('[data-app-theme-option]').length===1&&box.width>100&&
    box.left>=0&&box.right<=innerWidth+1&&picker.scrollWidth<=picker.clientWidth+1&&
    panel.scrollWidth<=panel.clientWidth+1&&document.documentElement.scrollWidth<=innerWidth+1;
})()`, width)
}

func selftestUIResponsiveThemes(ctx context.Context, window *mygo.Window, checks map[string]bool, capture func(string)) {
	originalBounds := window.Bounds()
	originalWidth, originalHeight := window.ContentSize()
	// The production minimum is 800x600. Only the isolated fixture lifts it to
	// exercise the stylesheet's mobile breakpoints, then restores that policy.
	window.SetMinimumSize(0, 0)
	defer func() {
		window.SetMinimumSize(800, 600)
		window.SetBounds(originalBounds)
		if ctx.Err() == nil {
			checks["interface theme selftest restores native window bounds"] = waitSelftestPage(ctx, window,
				fmt.Sprintf("innerWidth===%d&&innerHeight===%d", originalWidth, originalHeight), 5*time.Second)
		}
	}()
	if !uiThemeOpenAppearance(ctx, window) ||
		!waitSelftestPage(ctx, window, uiThemeChooseScript("harbor", "light", "app"), 5*time.Second) ||
		!waitSelftestPage(ctx, window, uiThemeOpenInbox, 5*time.Second) ||
		!waitSelftestPage(ctx, window, uiThemeSelectMessage, 5*time.Second) {
		return
	}
	for _, width := range []int{760, 390} {
		window.SetContentSize(width, originalHeight)
		checks[fmt.Sprintf("interface theme responsive reader at %dpx", width)] = waitSelftestPage(ctx, window,
			uiThemeNarrowReaderScript(width), 5*time.Second)
		capture(fmt.Sprintf("ui-responsive-%d-reader.png", width))
		if !waitSelftestPage(ctx, window, `(() => {
  const back=document.querySelector('.message-detail-back');
  if(!back||!back.getClientRects().length)return false;
  back.click();return true;
})()`, 5*time.Second) || !uiThemeOpenAppearance(ctx, window) {
			return
		}
		checks[fmt.Sprintf("interface theme responsive picker at %dpx", width)] = waitSelftestPage(ctx, window,
			uiThemeNarrowPickerScript(width), 5*time.Second)
		capture(fmt.Sprintf("ui-responsive-%d-picker.png", width))
		if !waitSelftestPage(ctx, window, uiThemeOpenInbox, 5*time.Second) ||
			!waitSelftestPage(ctx, window, uiThemeSelectMessage, 5*time.Second) {
			return
		}
	}
}

// Only the isolated desktop fixture calls this; all choices use production controls.
func selftestUIThemes(ctx context.Context, window *mygo.Window, checks map[string]bool, capture func(string)) {
	if os.Getenv("GOMAIL_SELFTEST") != "1" {
		return
	}
	for _, theme := range uiThemeSelftestMatrix {
		for _, mode := range []string{"light", "dark"} {
			checks[uiThemeSelftestCheck(theme.id, mode)] = false
		}
	}
	checks["interface theme picker exposes unified live preview"] = false
	checks["interface theme selftest restores initial appearance"] = false
	checks["interface theme selftest restores settings tab"] = false
	checks["interface theme selftest restores native window bounds"] = false
	for _, width := range []int{760, 390} {
		checks[fmt.Sprintf("interface theme responsive reader at %dpx", width)] = false
		checks[fmt.Sprintf("interface theme responsive picker at %dpx", width)] = false
	}

	var original struct {
		Theme        string `json:"theme"`
		Mode         string `json:"mode"`
		MessageTheme string `json:"messageTheme"`
		SettingsTab  string `json:"settingsTab"`
	}
	value, err := window.Page().EvalContext(ctx, `JSON.stringify((() => {
  const namespace=localStorage.getItem('gomail:profile-storage-migrated');
  const mode=namespace?localStorage.getItem('gomail:profile:'+namespace+':gomail-theme'):null;
  const settingsTab=namespace?sessionStorage.getItem('gomail:profile:'+namespace+':gomail-settings-tab'):null;
  return {theme:document.documentElement.dataset.uiTheme,mode:mode||'light',
    messageTheme:document.querySelector('[data-message-theme]')?.dataset.messageTheme||'app',
    settingsTab:settingsTab||'accounts'};
})())`)
	text, ok := value.(string)
	if err != nil || !ok || json.Unmarshal([]byte(text), &original) != nil {
		return
	}
	defer func() {
		if ctx.Err() != nil {
			return
		}
		if uiThemeOpenAppearance(ctx, window) &&
			waitSelftestPage(ctx, window, uiThemeChooseScript(original.Theme, original.Mode, original.MessageTheme), 5*time.Second) &&
			waitSelftestPage(ctx, window, uiThemeRestoreSettingsTabScript(original.SettingsTab), 5*time.Second) {
			checks["interface theme selftest restores settings tab"] = waitSelftestPage(ctx, window,
				uiThemeSettingsTabRestoredScript(original.SettingsTab), 5*time.Second)
			if waitSelftestPage(ctx, window, uiThemeOpenInbox, 5*time.Second) &&
				waitSelftestPage(ctx, window, uiThemeSelectMessage, 5*time.Second) {
				checks["interface theme selftest restores initial appearance"] = waitSelftestPage(ctx, window,
					fmt.Sprintf(`(() => {
  const preference=%s;
  const mode=preference==='system'?(matchMedia('(prefers-color-scheme: dark)').matches?'dark':'light'):preference;
  return document.documentElement.dataset.uiTheme===%s&&document.documentElement.dataset.theme===mode&&
    document.querySelector('[data-message-theme]')?.dataset.messageTheme===%s;
				})()`, strconv.Quote(original.Mode), strconv.Quote(original.Theme), strconv.Quote(original.MessageTheme)), 5*time.Second)
			}
		}
	}()
	for _, theme := range uiThemeSelftestMatrix {
		for _, mode := range []string{"light", "dark"} {
			if !uiThemeOpenAppearance(ctx, window) ||
				!waitSelftestPage(ctx, window, uiThemeChooseScript(theme.id, mode, "app"), 5*time.Second) {
				capture("ui-theme-failure.png")
				return
			}
			if theme.id == "harbor" && mode == "dark" {
				checks["interface theme picker exposes unified live preview"] = waitSelftestPage(ctx, window, `(() => {
  const picker=document.querySelector('.app-theme-picker');
  const panel=document.querySelector('.settings-panel-scroll');if(panel)panel.scrollTop=0;
  const darkBtn=document.querySelector('[data-color-mode="dark"]');
  return picker?.querySelectorAll('[data-app-theme-option]').length===1&&
    picker.querySelector('[data-app-theme-option="harbor"]')?.getAttribute('data-active')==='true'&&
    darkBtn?.getAttribute('aria-pressed')==='true'&&
    document.documentElement.dataset.theme==='dark'&&
    !document.documentElement.hasAttribute('data-theme-switching');
})()`, 5*time.Second)
				capture("ui-theme-picker.png")
			}
			if !waitSelftestPage(ctx, window, uiThemeOpenInbox, 5*time.Second) ||
				!waitSelftestPage(ctx, window, uiThemeSelectMessage, 5*time.Second) {
				capture("ui-theme-failure.png")
				return
			}
			checks[uiThemeSelftestCheck(theme.id, mode)] = waitSelftestPage(ctx, window,
				uiThemeRenderedScript(theme.id, mode, theme.rowHeight), 5*time.Second)
			if theme.id == "harbor" && mode == "dark" {
				checks["interface theme harbor dark sidebar uses dark layers"] = waitSelftestPage(ctx, window,
					uiThemeGlassDarkSidebarScript(), 5*time.Second)
			}
			capture("ui-" + theme.id + "-" + mode + ".png")
		}
	}
	selftestUIResponsiveThemes(ctx, window, checks, capture)
}
