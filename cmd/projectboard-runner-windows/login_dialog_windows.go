//go:build windows

package main

import (
	"strings"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
)

type loginInput struct {
	ServerURL string
	AgentKey  string
}

type loginDialogState struct {
	dialog                    *walk.Dialog
	serverEdit, keyEdit       *walk.LineEdit
	errorLabel                *walk.Label
	loginButton, cancelButton *walk.PushButton
	input                     loginInput
	accepted                  bool
}

func newLoginDialog(initialServerURL string) (*loginDialogState, Dialog) {
	if strings.TrimSpace(initialServerURL) == "" {
		initialServerURL = "http://127.0.0.1:3333"
	}

	state := &loginDialogState{}
	declaration := Dialog{
		AssignTo:      &state.dialog,
		Title:         "登录 ProjectBoard",
		DefaultButton: &state.loginButton,
		CancelButton:  &state.cancelButton,
		FixedSize:     true,
		Size:          Size{Width: 500, Height: 285},
		Layout:        VBox{Margins: Margins{Left: 22, Top: 18, Right: 22, Bottom: 18}, Spacing: 8},
		Children: []Widget{
			Label{Text: "输入 ProjectBoard 主页地址和 Agent Key，Runner 将自动登录并开始轮询。"},
			VSpacer{Size: 4},
			Label{Text: "ProjectBoard 主页地址"},
			LineEdit{
				AssignTo:  &state.serverEdit,
				Text:      initialServerURL,
				CueBanner: "https://projectboard.example.com",
			},
			Label{Text: "Agent Key"},
			LineEdit{
				AssignTo:     &state.keyEdit,
				PasswordMode: true,
				CueBanner:    "在 ProjectBoard 的 Agent 设置中生成",
			},
			Label{AssignTo: &state.errorLabel, TextColor: walk.RGB(184, 70, 79)},
			VSpacer{Size: 2},
			Composite{
				Layout: HBox{},
				Children: []Widget{
					HSpacer{},
					PushButton{
						AssignTo: &state.loginButton,
						Text:     "登录",
						OnClicked: func() {
							server := strings.TrimSpace(state.serverEdit.Text())
							key := strings.TrimSpace(state.keyEdit.Text())
							if server == "" {
								state.errorLabel.SetText("请输入 ProjectBoard 主页地址。")
								state.serverEdit.SetFocus()
								return
							}
							if key == "" {
								state.errorLabel.SetText("请输入 Agent Key。")
								state.keyEdit.SetFocus()
								return
							}
							state.input = loginInput{ServerURL: server, AgentKey: key}
							state.accepted = true
							state.dialog.Accept()
						},
					},
					PushButton{
						AssignTo:  &state.cancelButton,
						Text:      "取消",
						OnClicked: func() { state.dialog.Cancel() },
					},
				},
			},
		},
	}
	return state, declaration
}

func showLoginDialog(initialServerURL string) (loginInput, bool, error) {
	state, declaration := newLoginDialog(initialServerURL)
	_, err := declaration.Run(nil)
	if err != nil {
		return loginInput{}, false, err
	}
	return state.input, state.accepted, nil
}

func showLoginSucceeded(serverURL string) {
	walk.MsgBox(nil, "ProjectBoard Runner", "登录成功。Runner 已连接到：\n"+serverURL, walk.MsgBoxOK|walk.MsgBoxIconInformation)
}

func showLoginFailed(message string) {
	showRunnerError("ProjectBoard Runner 登录失败", message)
}

func showRunnerError(title, message string) {
	walk.MsgBox(nil, title, message, walk.MsgBoxOK|walk.MsgBoxIconError)
}
