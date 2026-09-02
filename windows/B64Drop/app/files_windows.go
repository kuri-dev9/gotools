// +build windows

package main

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
	"unicode/utf8"
	"unsafe"
)

const previewLimit = 64 * 1024

type fileEntry struct {
	name, path string
	size       int64
	modified   time.Time
}

func insertColumn(index int32, title string, width int32) {
	column := lvColumn{Mask: lvcfText | lvcfWidth | lvcfFmt, Format: lvcfmtLeft, Width: width, Text: utf16Ptr(title)}
	procSendMessage.Call(app.fileList, lvmInsertColumnW, uintptr(index), uintptr(unsafe.Pointer(&column)))
}

func insertListItem(row int32, values ...string) {
	for column, value := range values {
		item := lvItem{Mask: lvifText, Item: row, SubItem: int32(column), Text: utf16Ptr(value)}
		message := uintptr(lvmSetItemW)
		if column == 0 {
			message = lvmInsertItemW
		}
		procSendMessage.Call(app.fileList, message, 0, uintptr(unsafe.Pointer(&item)))
	}
}

func refreshFiles(selectPath string) {
	if err := os.MkdirAll(app.cfg.OutputDir, 0755); err != nil {
		setStatus("✕ Failed", err.Error())
		return
	}
	entries, err := ioutil.ReadDir(app.cfg.OutputDir)
	if err != nil {
		setStatus("✕ Failed", err.Error())
		return
	}
	files := make([]fileEntry, 0, len(entries))
	for _, info := range entries {
		if !info.Mode().IsRegular() || strings.HasPrefix(info.Name(), ".b64drop_tmp_") {
			continue
		}
		files = append(files, fileEntry{name: info.Name(), path: filepath.Join(app.cfg.OutputDir, info.Name()), size: info.Size(), modified: info.ModTime()})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].modified.After(files[j].modified) })
	app.files = files
	app.selected = -1
	procSendMessage.Call(app.fileList, lvmDeleteAllItems, 0, 0)
	selected := -1
	for i, file := range files {
		insertListItem(int32(i), file.name, humanSize(file.size), file.modified.Format("2006-01-02 15:04:05"))
		if selectPath != "" && samePath(file.path, selectPath) {
			selected = i
		}
	}
	if selected >= 0 {
		app.selected = selected
		item := lvItem{StateMask: lvisSelected | lvisFocused, State: lvisSelected | lvisFocused}
		procSendMessage.Call(app.fileList, lvmSetItemState, uintptr(selected), uintptr(unsafe.Pointer(&item)))
		procSendMessage.Call(app.fileList, lvmEnsureVisible, uintptr(selected), 0)
		previewFile(selected)
	} else {
		clearPreview()
	}
}

func samePath(left, right string) bool {
	return strings.EqualFold(filepath.Clean(left), filepath.Clean(right))
}

func handleNotify(lparam uintptr) {
	if lparam == 0 {
		return
	}
	notify := (*nmListView)(unsafe.Pointer(lparam))
	if notify.Header.HwndFrom != app.fileList || notify.Header.Code != lvnItemChanged {
		return
	}
	if notify.Item >= 0 && notify.NewState&lvisSelected != 0 && notify.OldState&lvisSelected == 0 {
		app.selected = int(notify.Item)
		previewFile(int(notify.Item))
	}
}

func selectedFile() (fileEntry, bool) {
	if app.selected < 0 || app.selected >= len(app.files) {
		return fileEntry{}, false
	}
	return app.files[app.selected], true
}

func previewFile(index int) {
	if index < 0 || index >= len(app.files) {
		clearPreview()
		return
	}
	file := app.files[index]
	handle, err := os.Open(file.path)
	if err != nil {
		setText(app.preview, "미리보기를 열 수 없습니다: "+err.Error())
		return
	}
	defer handle.Close()
	buffer := make([]byte, previewLimit+1)
	n, readErr := io.ReadFull(handle, buffer)
	if readErr != nil && readErr != io.EOF && readErr != io.ErrUnexpectedEOF {
		setText(app.preview, "미리보기를 읽을 수 없습니다: "+readErr.Error())
		return
	}
	truncated := n > previewLimit
	if truncated {
		n = previewLimit
	}
	data := buffer[:n]
	if bytes.HasPrefix(data, []byte{0xef, 0xbb, 0xbf}) {
		data = data[3:]
	}
	if truncated {
		for len(data) > 0 && !utf8.Valid(data) {
			data = data[:len(data)-1]
		}
	}
	setText(app.previewTitle, "미리보기: "+file.name)
	if !isSafeUTF8(data) {
		setText(app.preview, fmt.Sprintf("Binary file\r\n\r\nSize: %s\r\nPreview: unavailable", humanSize(file.size)))
	} else {
		text := strings.ReplaceAll(string(data), "\r\n", "\n")
		text = strings.ReplaceAll(text, "\n", "\r\n")
		setText(app.preview, text)
	}
	verified := ""
	if app.verified[filepath.Clean(file.path)] {
		verified = " / SHA-256 OK"
	}
	limit := ""
	if truncated || file.size > previewLimit {
		limit = " / 파일 앞부분 64 KiB만 표시"
	}
	setText(app.fileInfo, fmt.Sprintf("%s / %s / %s%s%s", file.name, humanSize(file.size), file.modified.Format("2006-01-02 15:04:05"), verified, limit))
}

func isSafeUTF8(data []byte) bool {
	if !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
		return false
	}
	for _, r := range string(data) {
		if r < 0x20 && r != '\r' && r != '\n' && r != '\t' {
			return false
		}
	}
	return true
}

func clearPreview() {
	setText(app.previewTitle, "미리보기")
	setText(app.preview, "파일을 선택하세요.")
	setText(app.fileInfo, "")
}

func handleCommand(command uint32) {
	switch command {
	case idRefresh:
		refreshFiles("")
	case idOpenFolder:
		openPath(app.cfg.OutputDir)
	case idChangeFolder:
		if atomic.LoadUint32(&app.processing) != 0 {
			showError("파일 복원 중에는 저장 위치를 변경할 수 없습니다.")
			return
		}
		changeOutputFolder()
	case idOpenFile:
		if file, ok := selectedFile(); ok {
			openPath(file.path)
		}
	case idDeleteFile:
		deleteSelectedFile()
	}
}

func openPath(path string) {
	os.MkdirAll(app.cfg.OutputDir, 0755)
	result, _, _ := procShellExecute.Call(app.hwnd, uintptr(unsafe.Pointer(utf16Ptr("open"))), uintptr(unsafe.Pointer(utf16Ptr(path))), 0, 0, swShow)
	if result <= 32 {
		showError("열 수 없습니다: " + path)
	}
}

func changeOutputFolder() {
	buffer := make([]uint16, 32768)
	browse := browseInfo{Owner: app.hwnd, DisplayName: &buffer[0], Title: utf16Ptr("B64Drop 저장 폴더 선택"), Flags: bifReturnOnlyFSDirs | bifNewDialogStyle}
	pidl, _, _ := procSHBrowseForFolder.Call(uintptr(unsafe.Pointer(&browse)))
	if pidl == 0 {
		return
	}
	defer procCoTaskMemFree.Call(pidl)
	if ok, _, _ := procSHGetPathFromIDList.Call(pidl, uintptr(unsafe.Pointer(&buffer[0]))); ok == 0 {
		showError("선택한 폴더 경로를 확인할 수 없습니다.")
		return
	}
	path := filepath.Clean(syscall.UTF16ToString(buffer))
	if err := os.MkdirAll(path, 0755); err != nil {
		showError(err.Error())
		return
	}
	app.cfg.OutputDir = path
	if err := saveConfig(app.cfg); err != nil {
		showError("설정을 저장하지 못했습니다: " + err.Error())
		return
	}
	setText(app.outputPath, path)
	clearPreview()
	refreshFiles("")
	setStatus("● Clipboard Monitoring", "저장 위치가 변경되었습니다.")
}

func deleteSelectedFile() {
	file, ok := selectedFile()
	if !ok {
		return
	}
	relative, err := filepath.Rel(filepath.Clean(app.cfg.OutputDir), filepath.Clean(file.path))
	if err != nil || relative == "." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) || filepath.IsAbs(relative) {
		showError("출력 폴더 밖의 파일은 삭제할 수 없습니다.")
		return
	}
	info, err := os.Lstat(file.path)
	if err != nil {
		showError(err.Error())
		return
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		showError("일반 파일만 삭제할 수 있습니다.")
		return
	}
	question := fmt.Sprintf("%s 파일을 삭제하시겠습니까?", file.name)
	answer, _, _ := procMessageBox.Call(app.hwnd, uintptr(unsafe.Pointer(utf16Ptr(question))), uintptr(unsafe.Pointer(utf16Ptr("B64Drop"))), mbYesNo|mbIconQuestion)
	if answer != idYes {
		return
	}
	if err = os.Remove(file.path); err != nil {
		showError("삭제하지 못했습니다: " + err.Error())
		return
	}
	delete(app.verified, filepath.Clean(file.path))
	clearPreview()
	refreshFiles("")
	setStatus("✓ Completed", file.name+" 파일을 삭제했습니다.")
}

func humanSize(size int64) string {
	const kb = 1024
	const mb = kb * 1024
	const gb = mb * 1024
	if size >= gb {
		return fmt.Sprintf("%.1f GB", float64(size)/gb)
	}
	if size >= mb {
		return fmt.Sprintf("%.1f MB", float64(size)/mb)
	}
	if size >= kb {
		return fmt.Sprintf("%.1f KB", float64(size)/kb)
	}
	return fmt.Sprintf("%d bytes", size)
}
