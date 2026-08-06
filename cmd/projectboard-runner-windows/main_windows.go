//go:build windows

package main

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/getlantern/systray"
	"github.com/projectboard/projectboard/internal/runnercli"
	"golang.org/x/sys/windows"
)

const instanceMutexName = `Local\ProjectBoardRunnerTray`

var instanceMutex windows.Handle

type trayManager struct {
	mu        sync.Mutex
	app       *runnercli.App
	logFile   *os.File
	cancel    context.CancelFunc
	done      chan struct{}
	running   bool
	loggingIn bool
	status    *systray.MenuItem
	login     *systray.MenuItem
	open      *systray.MenuItem
	start     *systray.MenuItem
	stop      *systray.MenuItem
	restart   *systray.MenuItem
	pause     *systray.MenuItem
	agentMenu *systray.MenuItem
	codex     *systray.MenuItem
	opencode  *systray.MenuItem
	openLogs  *systray.MenuItem
	openData  *systray.MenuItem
	autoStart *systray.MenuItem
	quit      *systray.MenuItem
}

func main() {
	if len(os.Args) > 1 {
		// This path is useful for development. Release builds use windowsgui and
		// ship the platform-independent projectboard-runner-cli.exe for terminals.
		app := runnercli.New(runnercli.Options{Out: os.Stdout, Err: os.Stderr})
		if err := app.Run(context.Background(), os.Args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if !acquireSingleInstance() {
		return
	}
	manager, err := newTrayManager()
	if err != nil {
		windows.CloseHandle(instanceMutex)
		return
	}
	systray.Run(manager.onReady, manager.onExit)
}

func newTrayManager() (*trayManager, error) {
	probe := runnercli.New(runnercli.Options{})
	info, err := probe.Info()
	if err != nil {
		return nil, err
	}
	if fileInfo, statErr := os.Stat(info.LogPath); statErr == nil && fileInfo.Size() > 2<<20 {
		archive := fmt.Sprintf("%s.%s", info.LogPath, time.Now().Format("20060102-150405"))
		_ = os.Rename(info.LogPath, archive)
	}
	logFile, err := os.OpenFile(info.LogPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	manager := &trayManager{logFile: logFile}
	manager.app = runnercli.New(runnercli.Options{
		Out: io.MultiWriter(logFile), Err: io.MultiWriter(logFile), OnPoll: manager.onPoll,
	})
	return manager, nil
}

func (m *trayManager) onReady() {
	systray.SetIcon(trayIcon())
	systray.SetTooltip("ProjectBoard Runner")
	m.status = systray.AddMenuItem("正在初始化", "Runner 当前状态")
	m.status.Disable()
	systray.AddSeparator()
	m.login = systray.AddMenuItem("登录", "输入 ProjectBoard 主页地址和 Agent Key")
	m.open = systray.AddMenuItem("打开 ProjectBoard", "在默认浏览器中打开 ProjectBoard")
	m.start = systray.AddMenuItem("启动 Runner", "开始轮询 ProjectBoard")
	m.stop = systray.AddMenuItem("停止 Runner", "停止轮询，但保留托盘程序")
	m.restart = systray.AddMenuItem("重启 Runner", "重新启动轮询")
	m.pause = systray.AddMenuItem("暂停领取", "暂停 Runner 的服务器轮询")
	m.agentMenu = systray.AddMenuItem("本地 Agent", "选择 Runner 执行任务时使用的本地编码 CLI")
	m.codex = m.agentMenu.AddSubMenuItemCheckbox("Codex CLI", "使用 Codex CLI 执行任务", false)
	m.opencode = m.agentMenu.AddSubMenuItemCheckbox("OpenCode CLI", "使用 OpenCode CLI 执行任务", false)
	m.refreshLocalAgentMenu()
	systray.AddSeparator()
	m.openLogs = systray.AddMenuItem("查看日志", "打开 Runner 日志")
	m.openData = systray.AddMenuItem("打开配置目录", "打开 Runner 当前用户配置目录")
	m.autoStart = systray.AddMenuItemCheckbox("开机自启", "登录 Windows 后自动启动 ProjectBoard Runner", false)
	if enabled, autoStartErr := runnerAutoStart.enabled(); autoStartErr != nil {
		fmt.Fprintf(m.logFile, "%s read auto-start setting: %v\n", time.Now().Format(time.RFC3339), autoStartErr)
	} else if enabled {
		m.autoStart.Check()
	}
	systray.AddSeparator()
	m.quit = systray.AddMenuItem("退出", "停止 Runner 并退出托盘程序")

	info, err := m.app.Info()
	if err != nil {
		m.setStatus("配置错误")
		m.setConnectedControls(false)
	} else if !info.Paired {
		m.setStatus("未登录，点击“登录”连接")
		m.setConnectedControls(false)
	} else {
		m.login.SetTitle("切换登录")
		m.setConnectedControls(true)
		m.updatePauseLabel(info.Paused)
		m.startRunner()
	}
	go m.handleClicks()
}

func (m *trayManager) handleClicks() {
	for {
		select {
		case <-m.login.ClickedCh:
			go m.loginRunner()
		case <-m.open.ClickedCh:
			if info, err := m.app.Info(); err == nil && info.ServerURL != "" {
				openTarget(info.ServerURL)
			}
		case <-m.start.ClickedCh:
			m.startRunner()
		case <-m.stop.ClickedCh:
			m.stopRunner()
		case <-m.restart.ClickedCh:
			m.restartRunner()
		case <-m.pause.ClickedCh:
			m.togglePause()
		case <-m.codex.ClickedCh:
			m.selectLocalAgent("codex")
		case <-m.opencode.ClickedCh:
			m.selectLocalAgent("opencode")
		case <-m.openLogs.ClickedCh:
			if info, err := m.app.Info(); err == nil {
				openTarget(info.LogPath)
			}
		case <-m.openData.ClickedCh:
			if info, err := m.app.Info(); err == nil {
				openTarget(info.ConfigDir)
			}
		case <-m.autoStart.ClickedCh:
			m.toggleAutoStart()
		case <-m.quit.ClickedCh:
			m.stopRunner()
			systray.Quit()
			return
		}
	}
}

func (m *trayManager) refreshLocalAgentMenu() {
	items := map[string]*systray.MenuItem{"codex": m.codex, "opencode": m.opencode}
	for _, agent := range m.app.LocalAgents() {
		item := items[agent.ID]
		if item == nil {
			continue
		}
		if agent.Selected {
			item.Check()
		} else {
			item.Uncheck()
		}
		if agent.Installed {
			item.Enable()
			item.SetTitle(agent.Name)
		} else {
			item.Disable()
			item.SetTitle(agent.Name + "（未安装）")
		}
	}
}

func (m *trayManager) selectLocalAgent(id string) {
	if err := m.app.SetLocalAgent(id); err != nil {
		fmt.Fprintf(m.logFile, "%s select local agent: %v\n", time.Now().Format(time.RFC3339), err)
		showRunnerError("无法切换本地 Agent", shortError(err))
		return
	}
	m.refreshLocalAgentMenu()
}

func (m *trayManager) toggleAutoStart() {
	m.autoStart.Disable()
	defer m.autoStart.Enable()

	enable := !m.autoStart.Checked()
	command := ""
	var err error
	if enable {
		command, err = currentAutoStartCommand()
	}
	if err == nil {
		err = runnerAutoStart.set(enable, command)
	}
	if err != nil {
		fmt.Fprintf(m.logFile, "%s update auto-start setting: %v\n", time.Now().Format(time.RFC3339), err)
		showRunnerError("无法更新开机自启", shortError(err))
		return
	}
	if enable {
		m.autoStart.Check()
	} else {
		m.autoStart.Uncheck()
	}
}

func (m *trayManager) loginRunner() {
	m.mu.Lock()
	if m.loggingIn {
		m.mu.Unlock()
		return
	}
	m.loggingIn = true
	m.login.Disable()
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		m.loggingIn = false
		m.login.Enable()
		m.mu.Unlock()
	}()

	initialServer := "http://127.0.0.1:3333"
	previousInfo, previousErr := m.app.Info()
	if previousErr == nil && previousInfo.ServerURL != "" {
		initialServer = previousInfo.ServerURL
	}
	input, accepted, err := showLoginDialog(initialServer)
	if err != nil {
		m.setStatus("无法打开登录窗口")
		fmt.Fprintf(m.logFile, "%s login dialog: %v\n", time.Now().Format(time.RFC3339), err)
		return
	}
	if !accepted {
		return
	}

	m.setStatus("正在登录")
	if err = m.app.Connect(context.Background(), input.ServerURL, input.AgentKey); err != nil {
		fmt.Fprintf(m.logFile, "%s login failed: %v\n", time.Now().Format(time.RFC3339), err)
		if previousErr == nil && previousInfo.Paired {
			m.setStatus("登录失败，原连接仍可用")
		} else {
			m.setStatus("登录失败")
		}
		showLoginFailed(shortError(err))
		return
	}

	m.stopRunner()
	_ = m.app.Run(context.Background(), []string{"resume"})
	m.login.SetTitle("切换登录")
	m.setConnectedControls(true)
	m.updatePauseLabel(false)
	m.startRunner()
	showLoginSucceeded(input.ServerURL)
}

func (m *trayManager) setConnectedControls(connected bool) {
	items := []*systray.MenuItem{m.open, m.start, m.stop, m.restart, m.pause}
	for _, item := range items {
		if connected {
			item.Enable()
		} else {
			item.Disable()
		}
	}
	if connected {
		m.stop.Disable()
		m.restart.Disable()
	}
}

func (m *trayManager) startRunner() {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	m.cancel, m.done, m.running = cancel, done, true
	m.start.Disable()
	m.stop.Enable()
	m.restart.Enable()
	m.mu.Unlock()

	go func() {
		err := m.app.Run(ctx, []string{"poll", "--watch"})
		if err != nil {
			fmt.Fprintf(m.logFile, "%s runner stopped: %v\n", time.Now().Format(time.RFC3339), err)
			m.setStatus("错误：" + shortError(err))
		}
		m.mu.Lock()
		if m.done == done {
			m.running = false
			m.cancel = nil
			m.start.Enable()
			m.stop.Disable()
		}
		close(done)
		m.mu.Unlock()
	}()
}

func (m *trayManager) stopRunner() {
	m.mu.Lock()
	cancel := m.cancel
	done := m.done
	if cancel == nil {
		m.mu.Unlock()
		return
	}
	m.setStatus("正在停止")
	cancel()
	m.mu.Unlock()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
	}
}

func (m *trayManager) restartRunner() {
	go func() {
		m.stopRunner()
		m.startRunner()
	}()
}

func (m *trayManager) togglePause() {
	info, err := m.app.Info()
	if err != nil {
		m.setStatus("配置错误")
		return
	}
	command := "pause"
	if info.Paused {
		command = "resume"
	}
	if err := m.app.Run(context.Background(), []string{command}); err != nil {
		m.setStatus("错误：" + shortError(err))
		return
	}
	m.updatePauseLabel(!info.Paused)
}

func (m *trayManager) onPoll(event runnercli.PollEvent) {
	switch event.Status {
	case runnercli.PollStarting:
		m.setStatus("正在连接")
	case runnercli.PollReady:
		m.setStatus("运行中")
	case runnercli.PollPaused:
		m.setStatus("已暂停")
		m.updatePauseLabel(true)
	case runnercli.PollError:
		m.setStatus("连接异常，正在重试")
	case runnercli.PollStopped:
		m.setStatus("已停止")
	}
}

func (m *trayManager) updatePauseLabel(paused bool) {
	if paused {
		m.pause.SetTitle("恢复领取")
		m.pause.SetTooltip("恢复 Runner 的服务器轮询")
	} else {
		m.pause.SetTitle("暂停领取")
		m.pause.SetTooltip("暂停 Runner 的服务器轮询")
	}
}

func (m *trayManager) setStatus(value string) {
	if m.status != nil {
		m.status.SetTitle(value)
	}
	systray.SetTooltip("ProjectBoard Runner · " + value)
}

func (m *trayManager) onExit() {
	m.stopRunner()
	if m.logFile != nil {
		_ = m.logFile.Close()
	}
	if instanceMutex != 0 {
		_ = windows.CloseHandle(instanceMutex)
	}
}

func acquireSingleInstance() bool {
	name, err := windows.UTF16PtrFromString(instanceMutexName)
	if err != nil {
		return false
	}
	instanceMutex, err = windows.CreateMutex(nil, false, name)
	if err != nil {
		return false
	}
	if errors.Is(windows.GetLastError(), windows.ERROR_ALREADY_EXISTS) {
		_ = windows.CloseHandle(instanceMutex)
		instanceMutex = 0
		return false
	}
	return true
}

func openTarget(target string) {
	var command *exec.Cmd
	if len(target) > 8 && (target[:7] == "http://" || target[:8] == "https://") {
		command = exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", target)
	} else {
		command = exec.Command("explorer.exe", target)
	}
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	_ = command.Start()
}

func shortError(err error) string {
	message := err.Error()
	const limit = 56
	if len([]rune(message)) > limit {
		return string([]rune(message)[:limit]) + "…"
	}
	return message
}

// trayIcon returns a small ICO containing a terminal glyph. Keeping it in code
// gives source builds a graceful icon without a binary asset generator.
func trayIcon() []byte {
	const size = 32
	xorSize := size * size * 4
	andSize := size * size / 8
	data := make([]byte, 6+16+40+xorSize+andSize)
	binary.LittleEndian.PutUint16(data[2:], 1)
	binary.LittleEndian.PutUint16(data[4:], 1)
	data[6], data[7] = size, size
	binary.LittleEndian.PutUint16(data[10:], 1)
	binary.LittleEndian.PutUint16(data[12:], 32)
	binary.LittleEndian.PutUint32(data[14:], uint32(40+xorSize+andSize))
	binary.LittleEndian.PutUint32(data[18:], 22)
	binary.LittleEndian.PutUint32(data[22:], 40)
	binary.LittleEndian.PutUint32(data[26:], size)
	binary.LittleEndian.PutUint32(data[30:], size*2)
	binary.LittleEndian.PutUint16(data[34:], 1)
	binary.LittleEndian.PutUint16(data[36:], 32)
	binary.LittleEndian.PutUint32(data[42:], uint32(xorSize))
	pixels := data[62 : 62+xorSize]
	set := func(x, y int, b, g, r, a byte) {
		offset := ((size-1-y)*size + x) * 4
		pixels[offset], pixels[offset+1], pixels[offset+2], pixels[offset+3] = b, g, r, a
	}
	for y := 3; y < 29; y++ {
		for x := 3; x < 29; x++ {
			if (x < 6 || x > 25) && (y < 6 || y > 25) {
				continue
			}
			set(x, y, 45, 36, 24, 255)
		}
	}
	for i := 0; i < 7; i++ {
		set(9+i, 10+i, 245, 245, 245, 255)
		set(9+i, 22-i, 245, 245, 245, 255)
		set(10+i, 10+i, 245, 245, 245, 255)
		set(10+i, 22-i, 245, 245, 245, 255)
	}
	for x := 17; x < 25; x++ {
		set(x, 22, 245, 245, 245, 255)
		set(x, 23, 245, 245, 245, 255)
	}
	return data
}
