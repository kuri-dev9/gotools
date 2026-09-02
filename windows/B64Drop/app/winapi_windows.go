// +build windows

package main

import "syscall"

const (
	wmCreate                       = 0x0001
	wmDestroy                      = 0x0002
	wmSize                         = 0x0005
	wmClose                        = 0x0010
	wmCommand                      = 0x0111
	wmNotify                       = 0x004E
	wmSetFont                      = 0x0030
	wmClipboardUpdate              = 0x031D
	wmAppResult                    = 0x8001
	cfUnicodeText                  = 13
	wsOverlappedWindow             = 0x00CF0000
	wsVisible                      = 0x10000000
	wsChild                        = 0x40000000
	wsVScroll                      = 0x00200000
	wsHScroll                      = 0x00100000
	wsTabStop                      = 0x00010000
	wsBorder                       = 0x00800000
	wsExClientEdge                 = 0x00000200
	esMultiline                    = 0x0004
	esAutoVScroll                  = 0x0040
	esAutoHScroll                  = 0x0080
	esReadOnly                     = 0x0800
	bsPushButton                   = 0
	lvsReport                      = 0x0001
	lvsSingleSel                   = 0x0004
	lvsShowSelAlways               = 0x0008
	lvsExFullRowSelect             = 0x00000020
	lvmFirst                       = 0x1000
	lvmSetExtendedListViewStyle    = lvmFirst + 54
	lvmDeleteAllItems              = lvmFirst + 9
	lvmInsertColumnW               = lvmFirst + 97
	lvmInsertItemW                 = lvmFirst + 77
	lvmSetItemW                    = lvmFirst + 76
	lvmSetItemState                = lvmFirst + 43
	lvmEnsureVisible               = lvmFirst + 19
	pbmSetPos                      = 0x0402
	pbmSetRange32                  = 0x0406
	lvifText                       = 0x0001
	lvifState                      = 0x0008
	lvisSelected                   = 0x0002
	lvisFocused                    = 0x0001
	lvcfFmt                        = 0x0001
	lvcfWidth                      = 0x0002
	lvcfText                       = 0x0004
	lvcfmtLeft                     = 0
	lvnItemChanged                 = ^uint32(100)
	mbOK                           = 0
	mbIconError                    = 0x10
	mbYesNo                        = 0x04
	mbIconQuestion                 = 0x20
	idYes                          = 6
	swShow                         = 5
	colorWindow                    = 5
	defaultGUIFont                 = 17
	processQueryLimitedInformation = 0x1000
	bifReturnOnlyFSDirs            = 0x0001
	bifNewDialogStyle              = 0x0040
	bifNonewFolderButton           = 0x0200
	iccListViewClasses             = 0x00000001
	iccProgressClass               = 0x00000020
	flashwTray                     = 0x00000002
	flashwTimerNoFG                = 0x0000000C
	mbIconAsterisk                 = 0x00000040
)

const (
	idChangeFolder = 1001
	idOpenFolder   = 1002
	idRefresh      = 1003
	idOpenFile     = 1004
	idDeleteFile   = 1005
)

var (
	user32                            = syscall.NewLazyDLL("user32.dll")
	kernel32                          = syscall.NewLazyDLL("kernel32.dll")
	shell32                           = syscall.NewLazyDLL("shell32.dll")
	gdi32                             = syscall.NewLazyDLL("gdi32.dll")
	comctl32                          = syscall.NewLazyDLL("comctl32.dll")
	ole32                             = syscall.NewLazyDLL("ole32.dll")
	procDefWindowProc                 = user32.NewProc("DefWindowProcW")
	procRegisterClass                 = user32.NewProc("RegisterClassW")
	procCreateWindowEx                = user32.NewProc("CreateWindowExW")
	procGetMessage                    = user32.NewProc("GetMessageW")
	procTranslateMessage              = user32.NewProc("TranslateMessage")
	procDispatchMessage               = user32.NewProc("DispatchMessageW")
	procPostQuitMessage               = user32.NewProc("PostQuitMessage")
	procPostMessage                   = user32.NewProc("PostMessageW")
	procDestroyWindow                 = user32.NewProc("DestroyWindow")
	procMoveWindow                    = user32.NewProc("MoveWindow")
	procGetClientRect                 = user32.NewProc("GetClientRect")
	procSetWindowText                 = user32.NewProc("SetWindowTextW")
	procSendMessage                   = user32.NewProc("SendMessageW")
	procMessageBox                    = user32.NewProc("MessageBoxW")
	procMessageBeep                   = user32.NewProc("MessageBeep")
	procFlashWindowEx                 = user32.NewProc("FlashWindowEx")
	procAddClipboardFormatListener    = user32.NewProc("AddClipboardFormatListener")
	procRemoveClipboardFormatListener = user32.NewProc("RemoveClipboardFormatListener")
	procOpenClipboard                 = user32.NewProc("OpenClipboard")
	procCloseClipboard                = user32.NewProc("CloseClipboard")
	procGetClipboardData              = user32.NewProc("GetClipboardData")
	procGetClipboardOwner             = user32.NewProc("GetClipboardOwner")
	procGetWindowThreadProcessID      = user32.NewProc("GetWindowThreadProcessId")
	procGlobalLock                    = kernel32.NewProc("GlobalLock")
	procGlobalUnlock                  = kernel32.NewProc("GlobalUnlock")
	procGlobalSize                    = kernel32.NewProc("GlobalSize")
	procOpenProcess                   = kernel32.NewProc("OpenProcess")
	procCloseHandle                   = kernel32.NewProc("CloseHandle")
	procQueryFullProcessImageName     = kernel32.NewProc("QueryFullProcessImageNameW")
	procGetModuleHandle               = kernel32.NewProc("GetModuleHandleW")
	procGetStockObject                = gdi32.NewProc("GetStockObject")
	procShellExecute                  = shell32.NewProc("ShellExecuteW")
	procSHBrowseForFolder             = shell32.NewProc("SHBrowseForFolderW")
	procSHGetPathFromIDList           = shell32.NewProc("SHGetPathFromIDListW")
	procCoTaskMemFree                 = ole32.NewProc("CoTaskMemFree")
	procInitCommonControlsEx          = comctl32.NewProc("InitCommonControlsEx")
)

type point struct{ X, Y int32 }
type rect struct{ Left, Top, Right, Bottom int32 }
type message struct {
	Hwnd           uintptr
	Message        uint32
	WParam, LParam uintptr
	Time           uint32
	Pt             point
}
type wndClass struct {
	Style                              uint32
	WndProc                            uintptr
	ClsExtra, WndExtra                 int32
	Instance, Icon, Cursor, Background uintptr
	MenuName, ClassName                *uint16
}
type initCommonControls struct {
	Size    uint32
	Classes uint32
}
type nmhdr struct {
	HwndFrom uintptr
	IDFrom   uintptr
	Code     uint32
}
type nmListView struct {
	Header                      nmhdr
	Item, SubItem               int32
	NewState, OldState, Changed uint32
	Action                      point
	LParam                      uintptr
}
type lvColumn struct {
	Mask                               uint32
	Format                             int32
	Width                              int32
	Text                               *uint16
	TextMax                            int32
	SubItem                            int32
	Image                              int32
	Order                              int32
	MinWidth, DefaultWidth, IdealWidth int32
}
type lvItem struct {
	Mask                      uint32
	Item, SubItem             int32
	State, StateMask          uint32
	Text                      *uint16
	TextMax                   int32
	Image                     int32
	LParam                    uintptr
	Indent, GroupID           uint32
	Columns                   uint32
	ColumnsPtr, ColumnFormats uintptr
	Group                     int32
}
type browseInfo struct {
	Owner, Root             uintptr
	DisplayName, Title      *uint16
	Flags                   uint32
	Callback, LParam, Image uintptr
}
type flashWindowInfo struct {
	Size    uint32
	Hwnd    uintptr
	Flags   uint32
	Count   uint32
	Timeout uint32
}

func utf16Ptr(value string) *uint16 { pointer, _ := syscall.UTF16PtrFromString(value); return pointer }
