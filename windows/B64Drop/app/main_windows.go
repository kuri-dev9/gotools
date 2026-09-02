// +build windows

package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"

	"gtools/pkg/b64drop"
)

type restoreResult struct {
	path     string
	size     int64
	chunk    bool
	progress b64drop.ChunkProgress
	err      error
}

var app struct {
	hwnd, statusTitle, statusDetail, outputLabel, outputPath, listLabel, fileList uintptr
	previewTitle, preview, fileInfo                                               uintptr
	transferTitle, transferProgress, transferChunks                               uintptr
	changeFolder, openFolder, refresh, openFile, deleteFile                       uintptr
	cfg                                                                           config
	files                                                                         []fileEntry
	selected                                                                      int
	verified                                                                      map[string]bool
	processing                                                                    uint32
	lastPayload                                                                   [32]byte
	hasLastPayload                                                                bool
	resultMu                                                                      sync.Mutex
	result                                                                        *restoreResult
	transferRoot                                                                  string
	activeProgress                                                                b64drop.ChunkProgress
	failedChunks                                                                  map[string]map[int]bool
}

func main() {
	executable, err := os.Executable()
	if err != nil {
		return
	}
	app.cfg = loadConfig(executable)
	app.verified = make(map[string]bool)
	app.failedChunks = make(map[string]map[int]bool)
	app.selected = -1
	app.transferRoot = filepath.Join(os.TempDir(), "B64Drop", "transfers")
	_ = b64drop.CleanupTransfers(app.transferRoot, b64drop.IncompleteTransferMaxAge)
	controls := initCommonControls{Size: uint32(unsafe.Sizeof(initCommonControls{})), Classes: iccListViewClasses | iccProgressClass}
	procInitCommonControlsEx.Call(uintptr(unsafe.Pointer(&controls)))
	className := utf16Ptr("B64DropMainWindow")
	instance, _, _ := procGetModuleHandle.Call(0)
	wc := wndClass{WndProc: syscall.NewCallback(windowProc), Instance: instance, Background: colorWindow + 1, ClassName: className}
	if result, _, _ := procRegisterClass.Call(uintptr(unsafe.Pointer(&wc))); result == 0 {
		return
	}
	hwnd, _, _ := procCreateWindowEx.Call(0, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(utf16Ptr("B64Drop"))), wsOverlappedWindow|wsVisible, 100, 60, 1000, 760, 0, 0, instance, 0)
	if hwnd == 0 {
		return
	}
	app.hwnd = hwnd
	var msg message
	for {
		result, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(result) <= 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMessage.Call(uintptr(unsafe.Pointer(&msg)))
	}
}

func windowProc(hwnd uintptr, msg uint32, wparam, lparam uintptr) uintptr {
	switch msg {
	case wmCreate:
		createControls(hwnd)
		if ok, _, _ := procAddClipboardFormatListener.Call(hwnd); ok == 0 {
			showError("Clipboard listener를 시작할 수 없습니다.")
			procDestroyWindow.Call(hwnd)
			return 0
		}
		refreshFiles("")
		setStatus("● Clipboard Monitoring", "PuTTY에서 B64DROP 데이터를 기다리고 있습니다.")
		return 0
	case wmSize:
		layoutControls(hwnd)
		return 0
	case wmClipboardUpdate:
		handleClipboardEvent()
		return 0
	case wmAppResult:
		handleRestoreResult()
		return 0
	case wmCommand:
		handleCommand(uint32(wparam) & 0xffff)
		return 0
	case wmNotify:
		handleNotify(lparam)
		return 0
	case wmClose:
		procDestroyWindow.Call(hwnd)
		return 0
	case wmDestroy:
		procRemoveClipboardFormatListener.Call(hwnd)
		procPostQuitMessage.Call(0)
		return 0
	}
	result, _, _ := procDefWindowProc.Call(hwnd, uintptr(msg), wparam, lparam)
	return result
}

func createControls(hwnd uintptr) {
	app.statusTitle = createControl(0, "STATIC", "", wsChild|wsVisible, 0, hwnd)
	app.statusDetail = createControl(0, "STATIC", "", wsChild|wsVisible, 0, hwnd)
	app.outputLabel = createControl(0, "STATIC", "저장 위치", wsChild|wsVisible, 0, hwnd)
	app.outputPath = createControl(wsExClientEdge, "EDIT", app.cfg.OutputDir, wsChild|wsVisible|esReadOnly|wsBorder, 0, hwnd)
	app.changeFolder = createControl(0, "BUTTON", "변경", wsChild|wsVisible|wsTabStop|bsPushButton, idChangeFolder, hwnd)
	app.openFolder = createControl(0, "BUTTON", "폴더 열기", wsChild|wsVisible|wsTabStop|bsPushButton, idOpenFolder, hwnd)
	app.listLabel = createControl(0, "STATIC", "저장된 파일", wsChild|wsVisible, 0, hwnd)
	app.refresh = createControl(0, "BUTTON", "새로고침", wsChild|wsVisible|wsTabStop|bsPushButton, idRefresh, hwnd)
	app.transferTitle = createControl(0, "STATIC", "Chunk Transfer: 대기 중", wsChild|wsVisible, 0, hwnd)
	app.transferProgress = createControl(0, "msctls_progress32", "", wsChild|wsVisible, 0, hwnd)
	app.transferChunks = createControl(wsExClientEdge, "EDIT", "수신 중인 Chunk가 없습니다.", wsChild|wsVisible|wsVScroll|esMultiline|esAutoVScroll|esReadOnly, 0, hwnd)
	app.fileList = createControl(wsExClientEdge, "SysListView32", "", wsChild|wsVisible|wsTabStop|lvsReport|lvsSingleSel|lvsShowSelAlways, 0, hwnd)
	procSendMessage.Call(app.fileList, lvmSetExtendedListViewStyle, lvsExFullRowSelect, lvsExFullRowSelect)
	insertColumn(0, "파일명", 480)
	insertColumn(1, "크기", 130)
	insertColumn(2, "저장시간", 190)
	app.openFile = createControl(0, "BUTTON", "파일 열기", wsChild|wsVisible|wsTabStop|bsPushButton, idOpenFile, hwnd)
	app.deleteFile = createControl(0, "BUTTON", "삭제", wsChild|wsVisible|wsTabStop|bsPushButton, idDeleteFile, hwnd)
	app.previewTitle = createControl(0, "STATIC", "미리보기", wsChild|wsVisible, 0, hwnd)
	app.preview = createControl(wsExClientEdge, "EDIT", "파일을 선택하세요.", wsChild|wsVisible|wsVScroll|wsHScroll|esMultiline|esAutoVScroll|esAutoHScroll|esReadOnly, 0, hwnd)
	app.fileInfo = createControl(0, "STATIC", "", wsChild|wsVisible, 0, hwnd)
	font, _, _ := procGetStockObject.Call(defaultGUIFont)
	for _, handle := range []uintptr{app.statusTitle, app.statusDetail, app.outputLabel, app.outputPath, app.changeFolder, app.openFolder, app.transferTitle, app.transferProgress, app.transferChunks, app.listLabel, app.refresh, app.fileList, app.openFile, app.deleteFile, app.previewTitle, app.preview, app.fileInfo} {
		procSendMessage.Call(handle, wmSetFont, font, 1)
	}
}

func createControl(exStyle uintptr, class, text string, style uintptr, id uintptr, parent uintptr) uintptr {
	handle, _, _ := procCreateWindowEx.Call(exStyle, uintptr(unsafe.Pointer(utf16Ptr(class))), uintptr(unsafe.Pointer(utf16Ptr(text))), style, 0, 0, 0, 0, parent, id, 0, 0)
	return handle
}

func layoutControls(hwnd uintptr) {
	var bounds rect
	procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&bounds)))
	w, h := int32(bounds.Right), int32(bounds.Bottom)
	if w == 0 || h == 0 {
		return
	}
	if w < 640 {
		w = 640
	}
	if h < 650 {
		h = 650
	}
	move(app.statusTitle, 20, 16, w-40, 24)
	move(app.statusDetail, 36, 42, w-56, 22)
	move(app.outputLabel, 20, 68, w-40, 18)
	move(app.outputPath, 20, 88, w-220, 26)
	move(app.changeFolder, w-190, 87, 75, 28)
	move(app.openFolder, w-105, 87, 85, 28)
	move(app.transferTitle, 20, 130, w-40, 22)
	move(app.transferProgress, 20, 154, w-40, 20)
	move(app.transferChunks, 20, 180, w-40, 80)
	move(app.listLabel, 20, 272, w-140, 22)
	move(app.refresh, w-105, 266, 85, 28)
	listHeight := (h - 350) / 2
	if listHeight < 110 {
		listHeight = 110
	}
	move(app.fileList, 20, 300, w-40, listHeight)
	buttonY := int32(308) + listHeight
	move(app.openFile, w-205, buttonY, 90, 28)
	move(app.deleteFile, w-105, buttonY, 85, 28)
	previewY := buttonY + 42
	move(app.previewTitle, 20, previewY, w-40, 22)
	move(app.preview, 20, previewY+26, w-40, h-previewY-78)
	move(app.fileInfo, 20, h-38, w-40, 22)
}

func move(hwnd uintptr, x, y, width, height int32) {
	procMoveWindow.Call(hwnd, uintptr(x), uintptr(y), uintptr(width), uintptr(height), 1)
}

func setText(hwnd uintptr, text string) {
	procSetWindowText.Call(hwnd, uintptr(unsafe.Pointer(utf16Ptr(text))))
}
func setStatus(title, detail string) {
	setText(app.statusTitle, title)
	setText(app.statusDetail, detail)
}

func clipboardOwnerIsPutty() bool {
	owner, _, _ := procGetClipboardOwner.Call()
	if owner == 0 {
		return false
	}
	var pid uint32
	procGetWindowThreadProcessID.Call(owner, uintptr(unsafe.Pointer(&pid)))
	if pid == 0 {
		return false
	}
	handle, _, _ := procOpenProcess.Call(processQueryLimitedInformation, 0, uintptr(pid))
	if handle == 0 {
		return false
	}
	defer procCloseHandle.Call(handle)
	buffer := make([]uint16, 32768)
	size := uint32(len(buffer))
	ok, _, _ := procQueryFullProcessImageName.Call(handle, 0, uintptr(unsafe.Pointer(&buffer[0])), uintptr(unsafe.Pointer(&size)))
	return ok != 0 && strings.EqualFold(filepath.Base(syscall.UTF16ToString(buffer[:size])), "putty.exe")
}

func clipboardText() (string, error) {
	if ok, _, _ := procOpenClipboard.Call(app.hwnd); ok == 0 {
		return "", fmt.Errorf("clipboard is busy")
	}
	defer procCloseClipboard.Call()
	handle, _, _ := procGetClipboardData.Call(cfUnicodeText)
	if handle == 0 {
		return "", fmt.Errorf("clipboard has no Unicode text")
	}
	pointer, _, _ := procGlobalLock.Call(handle)
	if pointer == 0 {
		return "", fmt.Errorf("cannot lock clipboard")
	}
	defer procGlobalUnlock.Call(handle)
	byteSize, _, _ := procGlobalSize.Call(handle)
	if byteSize < 2 || byteSize%2 != 0 {
		return "", fmt.Errorf("clipboard Unicode text has an invalid size")
	}
	unitCount := int(byteSize / 2)
	units := (*[1 << 29]uint16)(unsafe.Pointer(pointer))[:unitCount:unitCount]
	length := 0
	for length < len(units) && units[length] != 0 {
		length++
	}
	if length == len(units) {
		return "", fmt.Errorf("clipboard Unicode text is not terminated")
	}
	return syscall.UTF16ToString(units[:length]), nil
}

func handleClipboardEvent() {
	if !clipboardOwnerIsPutty() || atomic.LoadUint32(&app.processing) != 0 {
		return
	}
	text, err := clipboardText()
	if err != nil {
		setStatus("✕ Failed", err.Error())
		return
	}
	chunkBegin := strings.Contains(text, b64drop.ChunkBeginMarker)
	chunkEnd := strings.Contains(text, b64drop.ChunkEndMarker)
	chunkHint := chunkBegin || chunkEnd || strings.Contains(text, "B64DROP CHUNK")
	v1Begin := strings.Contains(text, b64drop.BeginMarker)
	v1End := strings.Contains(text, b64drop.EndMarker)
	if chunkHint && !(chunkBegin && chunkEnd) {
		setStatus("✕ Failed", "Clipboard data incomplete: B64DROP Chunk를 다시 복사해주세요.")
		return
	}
	if (v1Begin || v1End) && !(v1Begin && v1End) {
		setStatus("✕ Failed", "Clipboard data incomplete: B64DROP을 다시 복사해주세요.")
		return
	}
	if !(chunkBegin && chunkEnd) && !(v1Begin && v1End) {
		return
	}
	chunkMode := chunkBegin && chunkEnd
	if !chunkMode {
		digest := sha256.Sum256([]byte(text))
		if app.hasLastPayload && digest == app.lastPayload {
			return
		}
		app.lastPayload, app.hasLastPayload = digest, true
	}
	if !atomic.CompareAndSwapUint32(&app.processing, 0, 1) {
		return
	}
	setStatus("● Processing", "B64DROP 데이터를 검증하고 있습니다.")
	outputDir := app.cfg.OutputDir
	go func() {
		result := &restoreResult{chunk: chunkMode}
		if chunkMode {
			result.progress, result.err = b64drop.ReceiveChunk(strings.NewReader(text), app.transferRoot, outputDir)
			if result.progress.Completed {
				result.path = result.progress.PublishedPath
				result.size = result.progress.Manifest.OriginalSize
			}
		} else {
			result.path, result.size, result.err = b64drop.RestoreEnvelope(strings.NewReader(text), outputDir)
		}
		app.resultMu.Lock()
		app.result = result
		app.resultMu.Unlock()
		procPostMessage.Call(app.hwnd, wmAppResult, 0, 0)
	}()
}

func handleRestoreResult() {
	app.resultMu.Lock()
	result := app.result
	app.result = nil
	app.resultMu.Unlock()
	atomic.StoreUint32(&app.processing, 0)
	if result == nil {
		return
	}
	if result.chunk {
		handleChunkResult(result)
		return
	}
	if result.err != nil {
		setStatus("✕ Failed", result.err.Error())
		return
	}
	app.verified[filepath.Clean(result.path)] = true
	setStatus("✓ Completed", filepath.Base(result.path))
	refreshFiles(result.path)
	notifyCompletion(filepath.Base(result.path))
}

func handleChunkResult(result *restoreResult) {
	progress := result.progress
	id := progress.Manifest.TransferID
	if id != "" {
		if result.err != nil && app.activeProgress.Manifest.TransferID == id {
			progress.Manifest = app.activeProgress.Manifest
			progress.Received = append([]int(nil), app.activeProgress.Received...)
			progress.Missing = append([]int(nil), app.activeProgress.Missing...)
		}
		if app.failedChunks[id] == nil {
			app.failedChunks[id] = make(map[int]bool)
		}
		if result.err != nil && progress.LastIndex > 0 {
			app.failedChunks[id][progress.LastIndex] = true
		}
		if result.err == nil && progress.LastIndex > 0 {
			delete(app.failedChunks[id], progress.LastIndex)
		}
		progress.Failed = progress.Failed[:0]
		for index := range app.failedChunks[id] {
			progress.Failed = append(progress.Failed, index)
		}
		progress.Failed = b64drop.SortUnique(progress.Failed)
		app.activeProgress = progress
		renderChunkProgress(progress)
	}
	if result.err != nil {
		if progress.LastIndex > 0 {
			setStatus("✕ Chunk Failed", fmt.Sprintf("Chunk %d: %v. 다시 복사해주세요.", progress.LastIndex, result.err))
		} else {
			setStatus("✕ Chunk Failed", result.err.Error())
		}
		return
	}
	if !progress.Completed {
		verb := "수신 완료"
		if progress.ReReceived {
			verb = "재수신 완료"
		}
		setStatus("✓ Chunk "+verb, fmt.Sprintf("%s: %d / %d", progress.Manifest.Filename, len(progress.Received), progress.Manifest.ChunkTotal))
		return
	}
	delete(app.failedChunks, id)
	app.verified[filepath.Clean(progress.PublishedPath)] = true
	setStatus("✓ 파일 수신 완료", filepath.Base(progress.PublishedPath)+" / SHA-256 OK")
	refreshFiles(progress.PublishedPath)
	notifyCompletion(filepath.Base(progress.PublishedPath))
}

func renderChunkProgress(progress b64drop.ChunkProgress) {
	total := progress.Manifest.ChunkTotal
	if total <= 0 {
		return
	}
	procSendMessage.Call(app.transferProgress, pbmSetRange32, 0, uintptr(total))
	procSendMessage.Call(app.transferProgress, pbmSetPos, uintptr(len(progress.Received)), 0)
	setText(app.transferTitle, fmt.Sprintf("수신 중: %s  |  원본 %s  |  %d / %d", progress.Manifest.Filename, humanSize(progress.Manifest.OriginalSize), len(progress.Received), total))
	received := make(map[int]bool)
	failed := make(map[int]bool)
	for _, index := range progress.Received {
		received[index] = true
	}
	for _, index := range progress.Failed {
		failed[index] = true
	}
	var lines strings.Builder
	for index := 1; index <= total; index++ {
		symbol := "…"
		if received[index] {
			symbol = "✓"
		}
		if failed[index] {
			symbol = "✕"
		}
		if progress.ReReceived && index == progress.LastIndex {
			symbol = "↻"
		}
		fmt.Fprintf(&lines, "%d %s", index, symbol)
		if index%5 == 0 || index == total {
			lines.WriteString("\r\n")
		} else {
			lines.WriteString("    ")
		}
	}
	missing := append([]int(nil), progress.Missing...)
	missing = append(missing, progress.Failed...)
	missing = b64drop.SortUnique(missing)
	if len(missing) > 0 {
		fmt.Fprintf(&lines, "Missing/Failed Chunks: %s", formatChunkNumbers(missing))
	}
	setText(app.transferChunks, lines.String())
}

func formatChunkNumbers(values []int) string {
	parts := make([]string, len(values))
	for i, value := range values {
		parts[i] = fmt.Sprint(value)
	}
	return strings.Join(parts, ", ")
}

func notifyCompletion(filename string) {
	procMessageBeep.Call(mbIconAsterisk)
	flash := flashWindowInfo{Size: uint32(unsafe.Sizeof(flashWindowInfo{})), Hwnd: app.hwnd, Flags: flashwTray | flashwTimerNoFG, Count: 3}
	procFlashWindowEx.Call(uintptr(unsafe.Pointer(&flash)))
}

func showError(message string) {
	procMessageBox.Call(app.hwnd, uintptr(unsafe.Pointer(utf16Ptr(message))), uintptr(unsafe.Pointer(utf16Ptr("B64Drop"))), mbOK|mbIconError)
}
