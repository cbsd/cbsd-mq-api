// CBSD Project 2013-2025
// K8s-bhyve project 2020-2025
// MyBee project 2021-2025
package main

import (
	"bufio"
	"crypto/md5"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
//	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

//	"gopkg.in/yaml.v3"
	"github.com/gorilla/mux"
	"golang.org/x/crypto/ssh"
)

var lock = sync.RWMutex{}
var config Config
var runscript string
var workdir string
var server_url string
var acl_enable bool
var spool_Dir string
var onetime_Dir string
var vm_Engine string

var clusterLimitMax int

const MAX_UPLOAD_SIZE = 1024 * 1024 // 1MB

type Response struct {
	Message string
}

// The cluster Type. Name of elements must match with jconf params
type Vm struct {
	Image         string `json:image,omitempty"`
	Type          string `json:type,omitempty"`
	Vm_os_type    string `json:vm_os_type,omitempty"`
	Vm_os_profile string `json:vm_os_profile,omitempty"`
	Jname         string `json:jname,omitempty"`
	Ram           string `json:ram,omitempty"`
	Cpus          int    `"cpus,omitempty"`
	Imgsize       string `"imgsize,omitempty"`
	Pubkey        string `"pubkey,omitempty"`
	PkgList       string `"pkglist,omitempty"`
	Extras        string `"extras,omitempty"`
	Recomendation string `"recomendation,omitempty"`
	Host_hostname string `"host_hostname,omitempty"`
	Email         string `"email,omitempty"`
	Callback      string `"callback,omitempty"`
}

// The cluster Type. Name of elements must match with jconf params
type Cluster struct {
	Image             string `json:image,omitempty"`
	K8s_name          string `json:jname,omitempty"`
	Init_masters      string `json:init_masters,omitempty"`
	Init_workers      string `json:init_workers,omitempty"`
	Master_vm_ram     string `json:master_vm_ram,omitempty"`
	Master_vm_cpus    string `"master_vm_cpus,omitempty"`
	Master_vm_imgsize string `"master_vm_imgsize,omitempty"`
	Worker_vm_ram     string `"worker_vm_ram,omitempty"`
	Worker_vm_cpus    string `"worker_vm_cpus,omitempty"`
	Worker_vm_imgsize string `"worker_vm_imgsize,omitempty"`
	Pv_enable         string `"pv_enable,omitempty"`
	Pv_size           string `"pv_size,omitempty"`
	Kubelet_master    string `"kubelet_master,omitempty"`
	Email             string `"email,omitempty"`
	Callback          string `"callback,omitempty"`
	Pubkey            string `"pubkey,omitempty"`
	Recomendation     string `"recomendation,omitempty"`
}

// Todo: validate mod?
//  e.g for simple check:
//  bhyve_name  string `json:"name" validate:"required,min=2,max=100"`
var (
	body                   = flag.String("body", "", "Body of message")
	cbsdEnv                = flag.String("cbsdenv", "/usr/jails", "CBSD workdir environment")
	configFile             = flag.String("config", "/usr/local/etc/cbsd-mq-api.json", "Path to config.json")
	listen         *string = flag.String("listen", "0.0.0.0:65531", "Listen host:port")
	runScriptJail          = flag.String("runscript_jail", "jail-api", "CBSD target run script")
	runScriptVm            = flag.String("runscript_vm", "vm-api", "CBSD target run script")
	runScriptK8s           = flag.String("runscript_k8s", "k8world", "CBSD target run Kubernetes script")
	destroyScript          = flag.String("destroy_script", "control-api", "CBSD target run script")
	destroyK8sScript       = flag.String("destroy_k8s_script", "k8world", "CBSD target to destroy K8S")
	startScript            = flag.String("start_script", "control-api", "CBSD target run script")
	stopScript             = flag.String("stop_script", "control-api", "CBSD target run script")
	serverUrl              = flag.String("server_url", "http://127.0.0.1:65532", "Server URL for external requests")
	dbDir                  = flag.String("dbdir", "/var/db/cbsd-api", "db root dir")
	k8sDbDir               = flag.String("k8sdbdir", "/var/db/cbsd-k8s", "db root dir")
	allowListFile          = flag.String("allowlist", "", "Path to PubKey whitelist, e.g: -allowlist /usr/local/etc/cbsd-mq-api.allow")
	clusterLimit           = flag.Int("cluster_limit", 3, "Max number of clusters")
	spoolDir               = flag.String("spooldir", "/var/spool/cbsd-mq-api", "spool root dir")
	oneTimeConfDir         = flag.String("onetimeconfdir", "", "one-time config dir")
	vmEngine               = flag.String("vmengine", "bhyve", "VM engine: bhyve, qemu, virtualbox, xen")
)

type AllowList struct {
	keyType string
	key     string
	comment string
	cid     string
	next    *AllowList // link to the next records
}

// linked struct
type Feed struct {
	length int
	start  *AllowList
}

type MyFeeds struct {
	f *Feed
}

// Progress is used to track the progress of a file upload.
// It implements the io.Writer interface so it can be passed
// to an io.TeeReader()
type Progress struct {
	TotalSize int64
	BytesRead int64
}

// Write is used to satisfy the io.Writer interface.
// Instead of writing somewhere, it simply aggregates
// the total bytes on each read
func (pr *Progress) Write(p []byte) (n int, err error) {
	n, err = len(p), nil
	pr.BytesRead += int64(n)
	pr.Print()
	return
}

// Print displays the current progress of the file upload
func (pr *Progress) Print() {
	if pr.BytesRead == pr.TotalSize {
		fmt.Println("DONE!")
		return
	}

	fmt.Printf("File upload in progress: %d\n", pr.BytesRead)
}

func (f *Feed) Append(newAllow *AllowList) {
	if f.length == 0 {
		f.start = newAllow
	} else {
		currentPost := f.start
		for currentPost.next != nil {
			currentPost = currentPost.next
		}
		currentPost.next = newAllow
	}
	f.length++
}

func newAllow(keyType string, key string, comment string) *AllowList {

	KeyInList := fmt.Sprintf("%s %s %s", keyType, key, comment)
	uid := []byte(KeyInList)
	cid := md5.Sum(uid)

	cidString := fmt.Sprintf("%x", cid)

	np := AllowList{keyType: keyType, key: key, comment: comment, cid: cidString}
	//	np.Response = ""
	//	np.Time = 0
	return &np
}

// we need overwrite Content-Type here
// https://stackoverflow.com/questions/59763852/can-you-return-json-in-golang-http-error
func JSONError(w http.ResponseWriter, message string, code int) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// write header is mandatory to overwrite header
	w.WriteHeader(code)

	if len(message) > 0 {
		response := Response{message}
		js, err := json.Marshal(response)
		if err != nil {
			fmt.Fprintln(w, "{\"Message\":\"Marshal error\"}", http.StatusMethodNotAllowed)
			return
		}
		http.Error(w, string(js), code)
	} else {
		http.Error(w, "{}", http.StatusOK)
	}
	return
}

func fileExists(filename string) bool {
	_, err := os.Stat(filename)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("file does not exist", filename)
			return false
		} else {
			// error
			return false
		}
	} else {
		// file exist
		return true
	}
}

// main function to boot up everything
func main() {

	flag.Parse()
	var err error

	config, err = LoadConfiguration(*configFile)

	workdir = config.CbsdEnv
	server_url = config.ServerUrl
	spool_Dir = *spoolDir
	onetime_Dir = *oneTimeConfDir
	vm_Engine = *vmEngine

	if !fileExists(spool_Dir) {
		os.MkdirAll(spool_Dir, 0770)
	}

	clusterLimitMax = *clusterLimit

	if err != nil {
		fmt.Println("config load error")
		os.Exit(1)
	}

	if !fileExists(config.Recomendation) {
		fmt.Printf("no such Recomendation script, please check config/path: %s\n", config.Recomendation)
		os.Exit(1)
	}
	if !fileExists(config.Freejname) {
		fmt.Printf("no such Freejname script, please check config/path: %s\n", config.Freejname)
		os.Exit(1)
	}

	if !fileExists(*dbDir) {
		fmt.Printf("* db dir created: %s\n", *dbDir)
		os.MkdirAll(*dbDir, 0770)
	}

	if !fileExists(*k8sDbDir) {
		fmt.Printf("* db dir created: %s\n", *k8sDbDir)
		os.MkdirAll(*k8sDbDir, 0770)
	}

	f := &Feed{}

	fmt.Printf("* Cluster limit: %d\n", clusterLimitMax)
	fmt.Printf("* VM engine: %s\n", vm_Engine)

	// WhiteList
	if (*allowListFile == "") || (!fileExists(*allowListFile)) {
		fmt.Println("* no such allowList file ( -allowlist <path> )")
		fmt.Println("* ACL disabled: fully open system, all queries are permit!")
		acl_enable = false
	} else {
		fmt.Printf("* ACL enabled: %s\n", *allowListFile)
		acl_enable = true
		// loadconfig
		fd, err := os.Open(*allowListFile)
		if err != nil {
			panic(err)
		}
		defer fd.Close()

		scanner := bufio.NewScanner(fd)

		var keyType string
		var key string
		var comment string

		scanner.Split(bufio.ScanLines)
		var txtlines []string

		for scanner.Scan() {
			txtlines = append(txtlines, scanner.Text())
		}

		fd.Close()

		for _, eachline := range txtlines {
			fmt.Println(eachline)
			// todo: input validation
			// todo: auto-reload, signal
			_, err := fmt.Sscanf(eachline, "%s %s %s", &keyType, &key, &comment)
			if err != nil {
				log.Fatal(err)
				break
			}
			fmt.Printf("* ACL loaded: [%s %s %s]\n", keyType, key, comment)
			p := newAllow(keyType, key, comment)
			f.Append(p)
		}
		fmt.Printf("* AllowList Length: %v\n", f.length)
	}

	// setup: we need to pass Feed into handler function
	feeds := &MyFeeds{f: f}

	router := mux.NewRouter()
	router.HandleFunc("/api/v1/create/{InstanceId}", feeds.HandleClusterCreate).Methods("POST")
	router.HandleFunc("/api/v1/status/{InstanceId}", feeds.HandleClusterStatus).Methods("GET")
	router.HandleFunc("/api/v1/kubeconfig/{InstanceId}", feeds.HandleClusterKubeConfig).Methods("GET")
	router.HandleFunc("/api/v1/start/{InstanceId}", feeds.HandleClusterStart).Methods("GET")
	router.HandleFunc("/api/v1/stop/{InstanceId}", feeds.HandleClusterStop).Methods("GET")
	router.HandleFunc("/api/v1/destroy/{InstanceId}", feeds.HandleClusterDestroy).Methods("GET")
	router.HandleFunc("/api/v1/cluster", feeds.HandleClusterCluster).Methods("GET")
	router.HandleFunc("/api/v1/k8scluster", feeds.HandleK8sClusterCluster).Methods("GET")
//	for test only
//	router.HandleFunc("/api/v1/iac/{InstanceId}", feeds.HandleIac).Methods("POST")
//	router.HandleFunc("/api/v1/iac/{InstanceId}", feeds.HandleIacRequestStatus).Methods("GET")
	router.HandleFunc("/images", HandleClusterImages).Methods("GET")
	router.HandleFunc("/flavors", HandleClusterFlavors).Methods("GET")

	if len(onetime_Dir) > 1 {
		if !fileExists(onetime_Dir) {
			fmt.Printf("One-time directory not exist: %s\n", onetime_Dir)
			os.Exit(1)
		} else {
			fmt.Printf("* One-time dir enabled: %s\n", onetime_Dir)
			router.HandleFunc("/api/v1/otc/{CfgFile}", feeds.HandleOneTimeConf).Methods("GET")
		}
	} else {
		fmt.Println("* One-time dir disabled")
	}



	fmt.Println("* Listen", *listen)
	fmt.Println("* Server URL", server_url)
	log.Fatal(http.ListenAndServe(*listen, router))
}

func validateCid(Cid string) bool {
	var regexpCid = regexp.MustCompile("^[a-f0-9]{32}$")

	if regexpCid.MatchString(Cid) {
		return true
	} else {
		return false
	}
}

func validateInstanceId(InstanceId string) bool {
	var regexpInstanceId = regexp.MustCompile("^[a-z_]([a-z0-9_])*$")

	if len(InstanceId) < 1 || len(InstanceId) > 40 {
		return false
	}

	if regexpInstanceId.MatchString(InstanceId) {
		return true
	} else {
		return false
	}
}

func validateCfgFile(CfgFile string) bool {
	var regexpCfgFile = regexp.MustCompile("^[aA0-zZ9_]([aA0-zZ9_])*$")

	if len(CfgFile) < 1 || len(CfgFile) > 10 {
		return false
	}

	if regexpCfgFile.MatchString(CfgFile) {
		return true
	} else {
		return false
	}
}

func isPubKeyAllowed(feeds *MyFeeds, PubKey string) bool {
	//ALLOWED?
	var p *AllowList
	currentAllow := feeds.f.start

	if !acl_enable {
		return true
	}

	for i := 0; i < feeds.f.length; i++ {
		p = currentAllow
		currentAllow = currentAllow.next
		ResultKeyType := (string(p.keyType))
		ResultKey := (string(p.key))
		ResultKeyComment := (string(p.comment))
		//fmt.Println("ResultType: ", ResultKeyType)
		KeyInList := fmt.Sprintf("%s %s %s", ResultKeyType, ResultKey, ResultKeyComment)
		fmt.Printf("[%s][%s]\n", PubKey, KeyInList)

		if len(PubKey) == len(KeyInList) {
			if strings.Compare(PubKey, KeyInList) == 0 {
				fmt.Printf("pubkey matched\n")
				return true
			}
		}
	}

	return false
}

func isCidAllowed(feeds *MyFeeds, Cid string) bool {
	//ALLOWED?
	var p *AllowList
	currentAllow := feeds.f.start

	if !acl_enable {
		return true
	}

	for i := 0; i < feeds.f.length; i++ {
		p = currentAllow
		currentAllow = currentAllow.next
		CidInList := (string(p.cid))
		if strings.Compare(Cid, CidInList) == 0 {
			fmt.Printf("Cid ACL matched: %s\n", Cid)
			return true
		}
	}

	return false
}

func (feeds *MyFeeds) HandleClusterStatus(w http.ResponseWriter, r *http.Request) {
	var InstanceId string
	// enum { 0 - vm, 1 - k8s }
	var vmType int
	params := mux.Vars(r)

	InstanceId = params["InstanceId"]
	if !validateInstanceId(InstanceId) {
		JSONError(w, "The InstanceId should be valid form: ^[a-z_]([a-z0-9_])*$ (maxlen: 40)", http.StatusMethodNotAllowed)
		return
	}

	Cid := r.Header.Get("cid")
	if !validateCid(Cid) {
		JSONError(w, "The cid should be valid form: ^[a-f0-9]{32}$", http.StatusMethodNotAllowed)
		return
	}

	if !isCidAllowed(feeds, Cid) {
		fmt.Printf("CID not in ACL: %s\n", Cid)
		JSONError(w, "not allowed", http.StatusMethodNotAllowed)
		return
	}

	var mapfile string

	checkMapfile := fmt.Sprintf("%s/var/db/api/map/%s-%s", workdir, Cid, InstanceId)
	if _, err := os.Stat(checkMapfile); os.IsNotExist(err) {
		fmt.Printf("status: no such %s/%s/vms - check K8S...\n", *dbDir, Cid)
		// check K8S dir
		checkMapfile = fmt.Sprintf("%s/var/db/k8s/map/%s-%s", workdir, Cid, InstanceId)
		if _, err := os.Stat(checkMapfile); os.IsNotExist(err) {
			JSONError(w, "not found", http.StatusOK)
			return
		} else {
			fmt.Printf("%s found - its K8S\n", checkMapfile)
			// K8S instance
			vmType = 1
			mapfile = checkMapfile
		}
	} else {
		//VM/jail instance
		fmt.Printf("%s/%s/vms found - its not K8S\n", *dbDir, Cid)
		vmType = 0
		mapfile = checkMapfile
	}

	b, err := ioutil.ReadFile(mapfile) // just pass the file name
	if err != nil {
		fmt.Printf("unable to read jname from: [%s]/var/db/api/map/%s-%s\n", mapfile)
		JSONError(w, "not found", http.StatusOK)
		return
	}

	var SqliteDBPath string

	if ( vmType == 1 ) {
		SqliteDBPath = fmt.Sprintf("%s/%s/%s-vm.ssh", *k8sDbDir, Cid, string(b))
	} else {
		SqliteDBPath = fmt.Sprintf("%s/%s/%s-vm.ssh", *dbDir, Cid, string(b))
	}

	if fileExists(SqliteDBPath) {
		b, err := ioutil.ReadFile(SqliteDBPath) // just pass the file name
		if err != nil {
			JSONError(w, "", 400)
			return
		} else {
			// already in json - send as-is
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.WriteHeader(200)
			http.Error(w, string(b), 200)
			return
		}
	} else {
		JSONError(w, "", http.StatusOK)
	}
}

func (feeds *MyFeeds) HandleK8sClusterStatus(w http.ResponseWriter, r *http.Request) {
	var InstanceId string
	params := mux.Vars(r)

	InstanceId = params["InstanceId"]
	if !validateInstanceId(InstanceId) {
		JSONError(w, "The InstanceId should be valid form: ^[a-z_]([a-z0-9_])*$ (maxlen: 40)", http.StatusMethodNotAllowed)
		return
	}

	Cid := r.Header.Get("cid")
	if !validateCid(Cid) {
		JSONError(w, "The cid should be valid form: ^[a-f0-9]{32}$", http.StatusMethodNotAllowed)
		return
	}

	if !isCidAllowed(feeds, Cid) {
		fmt.Printf("CID not in ACL: %s\n", Cid)
		JSONError(w, "not allowed", http.StatusMethodNotAllowed)
		return
	}

	HomePath := fmt.Sprintf("%s/%s/vms", *k8sDbDir, Cid)
	if _, err := os.Stat(HomePath); os.IsNotExist(err) {
		JSONError(w, "not found", http.StatusOK)
		return
	}

	mapfile := fmt.Sprintf("%s/var/db/k8s/map/%s-%s", workdir, Cid, InstanceId)

	if !fileExists(config.Recomendation) {
		fmt.Printf("no such map file %s/var/db/k8s/map/%s-%s\n", workdir, Cid, InstanceId)
		JSONError(w, "not found", http.StatusOK)
		return
	}

	b, err := ioutil.ReadFile(mapfile) // just pass the file name
	if err != nil {
		fmt.Printf("unable to read jname from %s/var/db/k8s/map/%s-%s\n", workdir, Cid, InstanceId)
		JSONError(w, "not found", http.StatusOK)
		return
	}

	SqliteDBPath := fmt.Sprintf("%s/%s/%s-vm.ssh", *k8sDbDir, Cid, string(b))
	if fileExists(SqliteDBPath) {
		b, err := ioutil.ReadFile(SqliteDBPath) // just pass the file name
		if err != nil {
			JSONError(w, "", 400)
			return
		} else {
			// already in json - send as-is
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.WriteHeader(200)
			http.Error(w, string(b), 200)
			return
		}
	} else {
		JSONError(w, "", http.StatusOK)
	}
}

func (feeds *MyFeeds) HandleClusterKubeConfig(w http.ResponseWriter, r *http.Request) {
	var InstanceId string
	params := mux.Vars(r)

	InstanceId = params["InstanceId"]
	if !validateInstanceId(InstanceId) {
		JSONError(w, "The InstanceId should be valid form: ^[a-z_]([a-z0-9_])*$ (maxlen: 40)", http.StatusMethodNotAllowed)
		return
	}

	Cid := r.Header.Get("cid")
	if !validateCid(Cid) {
		JSONError(w, "The cid should be valid form: ^[a-f0-9]{32}$", http.StatusMethodNotAllowed)
		return
	}

	if !isCidAllowed(feeds, Cid) {
		fmt.Printf("CID not in ACL: %s\n", Cid)
		JSONError(w, "not allowed", http.StatusMethodNotAllowed)
		return
	}

	VmPath := fmt.Sprintf("%s/%s/cluster-%s", *k8sDbDir, Cid, InstanceId)

	if !fileExists(VmPath) {
		fmt.Printf("ClusterKubeConfig: Error read vmpath file  [%s]\n", VmPath)
		JSONError(w, "", 400)
		return
	}

	b, err := ioutil.ReadFile(VmPath) // just pass the file name
	if err != nil {
		fmt.Printf("Error read vmpath file  [%s]\n", VmPath)
		JSONError(w, "", 400)
		return
	} else {
		kubeFile := fmt.Sprintf("%s/var/db/k8s/%s.kubeconfig", workdir, string(b))
		if fileExists(kubeFile) {
			b, err := ioutil.ReadFile(kubeFile) // just pass the file name
			if err != nil {
				fmt.Printf("unable to read content %s\n", kubeFile)
				JSONError(w, "", http.StatusOK)
				return
			}
			w.Header().Set("Content-Type", "text/plain")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.WriteHeader(200)
			http.Error(w, string(b), 200)
			return
		} else {
			fmt.Printf("Error read kubeconfig  [%s]\n", kubeFile)
			JSONError(w, "", 400)
			return
		}
	}
}

func (feeds *MyFeeds) HandleClusterCluster(w http.ResponseWriter, r *http.Request) {
	Cid := r.Header.Get("cid")
	if !validateCid(Cid) {
		JSONError(w, "The cid should be valid form: ^[a-f0-9]{32}$", http.StatusMethodNotAllowed)
		return
	}

	if !isCidAllowed(feeds, Cid) {
		fmt.Printf("CID not in ACL: %s\n", Cid)
		JSONError(w, "not allowed", http.StatusMethodNotAllowed)
		return
	}

	HomePath := fmt.Sprintf("%s/%s/vms", *dbDir, Cid)
	//fmt.Println("CID IS: [ %s ]", cid)
	
	if _, err := os.Stat(HomePath); os.IsNotExist(err) {
		JSONError(w, "", http.StatusOK)
		return
	}

	SqliteDBPath := fmt.Sprintf("%s/%s/vm.list", *dbDir, Cid)
	if fileExists(SqliteDBPath) {
		b, err := ioutil.ReadFile(SqliteDBPath) // just pass the file name
		if err != nil {
			JSONError(w, "", http.StatusOK)
			return
		} else {
			// already in json - send as-is
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.WriteHeader(200)
			http.Error(w, string(b), 200)
			return
		}
	} else {
		JSONError(w, "", http.StatusOK)
		return
	}
}

// read /var/db/cbsd-k8s/<cid>/vms/vm.list
func (feeds *MyFeeds) HandleK8sClusterCluster(w http.ResponseWriter, r *http.Request) {
	Cid := r.Header.Get("cid")
	if !validateCid(Cid) {
		JSONError(w, "The cid should be valid form: ^[a-f0-9]{32}$", http.StatusMethodNotAllowed)
		return
	}

	if !isCidAllowed(feeds, Cid) {
		fmt.Printf("CID not in ACL: %s\n", Cid)
		JSONError(w, "not allowed", http.StatusMethodNotAllowed)
		return
	}

	HomePath := fmt.Sprintf("%s/%s/vms", *k8sDbDir, Cid)
	//fmt.Println("CID IS: [ %s ]", cid)
	if _, err := os.Stat(HomePath); os.IsNotExist(err) {
		JSONError(w, "", http.StatusOK)
		return
	}

	SqliteDBPath := fmt.Sprintf("%s/%s/vm.list", *k8sDbDir, Cid)
	if fileExists(SqliteDBPath) {
		b, err := ioutil.ReadFile(SqliteDBPath) // just pass the file name
		if err != nil {
			JSONError(w, "", http.StatusOK)
			return
		} else {
			// already in json - send as-is
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.WriteHeader(200)
			http.Error(w, string(b), 200)
			return
		}
	} else {
		JSONError(w, "", http.StatusOK)
		return
	}
}

func HandleClusterImages(w http.ResponseWriter, r *http.Request) {

	if fileExists(config.Cloud_images_list) {
		b, err := ioutil.ReadFile(config.Cloud_images_list) // just pass the file name
		if err != nil {
			JSONError(w, "", http.StatusOK)
			return
		} else {
			// already in json - send as-is
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.WriteHeader(200)
			http.Error(w, string(b), 200)
			return
		}
	} else {
		JSONError(w, "", http.StatusOK)
		return
	}
}

func HandleClusterFlavors(w http.ResponseWriter, r *http.Request) {

	if fileExists(config.Flavors_list) {
		b, err := ioutil.ReadFile(config.Flavors_list) // just pass the file name
		if err != nil {
			JSONError(w, "", http.StatusOK)
			return
		} else {
			// already in json - send as-is
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.WriteHeader(200)
			http.Error(w, string(b), 200)
			return
		}
	} else {
		JSONError(w, "", http.StatusOK)
		return
	}
}

func realInstanceCreate(body string) {

	a := &body

	stdout, err := beanstalkSend(config.BeanstalkConfig, *a)
	fmt.Printf("%s\n", stdout)

	if err != nil {
		return
	}
}

func getStructTag(f reflect.StructField) string {
	return string(f.Tag)
}

func getNodeRecomendation(body string, offer string) {
	// offer - recomendation host from user, we can check them in external helper
	// for valid/resource

	var result string

	if len(offer) > 1 {
		result = offer
		fmt.Printf("FORCED Host Recomendation: [%s]\n", result)
	} else {
		cmdStr := fmt.Sprintf("%s %s", config.Recomendation, body)
		cmdArgs := strings.Fields(cmdStr)
		cmd := exec.Command(cmdArgs[0], cmdArgs[1:len(cmdArgs)]...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			fmt.Println("get recomendation script failed")
			return
		}
		result = (string(out))
	}

	fmt.Printf("Host Recomendation: [%s]\n", result)

	result = strings.Replace(result, ".", "_", -1)
	result = strings.Replace(result, "-", "_", -1)

	tube := fmt.Sprintf("cbsd_%s", result)
	reply := fmt.Sprintf("cbsd_%s_result_id", result)

	fmt.Printf("Tube selected: [%s]\n", tube)
	fmt.Printf("ReplyTube selected: [%s]\n", reply)

	config.BeanstalkConfig.Tube = tube
	config.BeanstalkConfig.ReplyTubePrefix = reply
}

func applyIac(env string, yaml string) {
	// offer - recomendation host from user, we can check them in external helper
	// for valid/resource

	var result string

	cmdStr := fmt.Sprintf("/usr/local/bin/cbsd-mq-api-apply %s /var/spool/cbsd-mq-api/upload/%s", env, yaml)
	cmdArgs := strings.Fields(cmdStr)
	cmd := exec.Command(cmdArgs[0], cmdArgs[1:len(cmdArgs)]...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Println("cbsd-mq-api-apply failed", cmdStr)
		return
	}
	result = (string(out))

	fmt.Printf("IaC Apply: [%s]\n", result)
}


func getJname() string {
	cmdStr := fmt.Sprintf("%s", config.Freejname)
	cmdArgs := strings.Fields(cmdStr)
	cmd := exec.Command(cmdArgs[0], cmdArgs[1:len(cmdArgs)]...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Println("get freejname script failed")
		return ""
	}
	result := (string(out))
	fmt.Printf("Freejname Recomendation: [%s]\n", result)
	return result
}

func getId(cid string) string {
	cmdStr := fmt.Sprintf("%s", config.Freeid)
	cmdArgs := strings.Fields(cmdStr)
//	cmd := exec.Command(cmdArgs[0], cmdArgs[1:len(cmdArgs)]...)
	cmd := exec.Command(cmdArgs[0], cid)
	out, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Println("get freeid script failed")
		return ""
	}
	result := (string(out))
	fmt.Printf("Freeid Recomendation: [%s]\n", result)
	return result
}


//func (feeds *MyFeeds) HandleClusterCluster(w http.ResponseWriter, r *http.Request) {
//func HandleClusterCreate(w http.ResponseWriter, r *http.Request) {
//func (feeds *MyFeeds) 

//func HandleCreateVm(w http.ResponseWriter, r *http.Request ) {
func HandleCreateVm(w http.ResponseWriter, vm Vm) {

	var regexpPkgList = regexp.MustCompile(`^[aA-zZ_]([aA-zZ0-9_\-/ ])*$`)
	var regexpExtras = regexp.MustCompile("^[a-zA-Z0-9:,]*$")
	var regexpSize = regexp.MustCompile(`^[1-9](([0-9]+)?)([m|g|t])$`)
	var regexpParamName = regexp.MustCompile(`^[a-z_]+$`)
	var regexpParamVal = regexp.MustCompile(`^[aA-zZ0-9_\-. ]+$`)
	var regexpHostName = regexp.MustCompile(`^[aA-zZ0-9_\-\.]+$`)
	var suggest string
	var InstanceId string

	InstanceId = vm.Jname

	uid := []byte(vm.Pubkey)

	//existance?
	// check for existance
	cid := md5.Sum(uid)

	VmPathDir := fmt.Sprintf("%s/%x", *dbDir, cid)

	if !fileExists(VmPathDir) {
		os.Mkdir(VmPathDir, 0775)
	}

	VmPath := fmt.Sprintf("%s/%x/vm-%s", *dbDir, cid, InstanceId)

	if fileExists(VmPath) {
		fmt.Printf("Error: vm already exist: [%s]\n", VmPath)
		JSONError(w, "vm already exist", http.StatusMethodNotAllowed)
		return
	}

	fmt.Printf("vm file not exist, create empty: [%s]\n", VmPath)
	// create empty file
	f, err := os.Create(VmPath)

	if err != nil {
		log.Fatal(err)
	}

	if len(vm.PkgList) > 1 {
		if !regexpPkgList.MatchString(vm.PkgList) {
			fmt.Printf("Error: wrong pkglist: [%s]\n", vm.PkgList)
			JSONError(w, "pkglist should be valid form. valid form", http.StatusMethodNotAllowed)
			return
		}
	}

	if len(vm.Host_hostname) > 1 {
		if !regexpHostName.MatchString(vm.Host_hostname) {
			fmt.Printf("Error: wrong hostname: [%s]\n", vm.Host_hostname)
			JSONError(w, "host_hostname should be valid form. valid form", http.StatusMethodNotAllowed)
			return
		} else {
			fmt.Printf("Found host_hostname: [%s]\n", vm.Host_hostname)
		}
	}

	if len(vm.Extras) > 1 {
		if !regexpExtras.MatchString(vm.Extras) {
			fmt.Printf("Error: wrong extras: [%s]\n", vm.Extras)
			JSONError(w, "extras should be valid form. valid form", http.StatusMethodNotAllowed)
			return
		} else {
			fmt.Printf("Found extras: [%s]\n", vm.Extras)
		}
	}

	if len(vm.Recomendation) > 1 {
		if !regexpHostName.MatchString(vm.Recomendation) {
			fmt.Printf("Error: wrong hostname recomendation: [%s]\n", vm.Recomendation)
			JSONError(w, "recomendation should be valid form. valid form", http.StatusMethodNotAllowed)
			return
		} else {
			fmt.Printf("Found vm recomendation: [%s]\n", vm.Recomendation)
			suggest = vm.Recomendation
		}
	} else {
		suggest = ""
	}

	if vm.Cpus <= 0 || vm.Cpus > 16 {
		JSONError(w, "cpus valid range: 1-16", http.StatusMethodNotAllowed)
		return
	}

	if len(vm.Ram) > 0 {
		if !regexpSize.MatchString(vm.Ram) {
			JSONError(w, "The ram should be valid form, 512m, 1g", http.StatusMethodNotAllowed)
			return
		}
	} else {
		// unlimited for jail
		vm.Ram = "0"
	}

	switch vm.Image {
		case "jail":
			//Imgsize optional for jail type
			if len(vm.Imgsize) > 0 {
				if !regexpSize.MatchString(vm.Imgsize) {
					fmt.Printf("wrong imgsize: [%s] [%d]\n", vm.Imgsize, vm.Imgsize)
					JSONError(w, "The imgsize should be valid form: 2g, 30g", http.StatusMethodNotAllowed)
					return
				}
			}
		default:
			if !regexpSize.MatchString(vm.Imgsize) {
				fmt.Printf("wrong imgsize: [%s] [%d]\n", vm.Imgsize, vm.Imgsize)
				JSONError(w, "The imgsize should be valid form: 2g, 30g", http.StatusMethodNotAllowed)
				return
			}
	}

	Jname := getJname()
	if len(Jname) < 1 {
		log.Fatal("unable to get jname")
		return
	}

	fmt.Printf("GET NEXT FREE JNAME: [%s]\n", Jname)

	_, err2 := f.WriteString(Jname)

	if err2 != nil {
		log.Fatal(err2)
	}

	f.Close()

	vm.Jname = InstanceId
	val := reflect.ValueOf(vm)

	var jconf_param string
	var str strings.Builder
	var recomendation strings.Builder
	// of course we can use marshal here instead of string concatenation,
	// but now this is too simple case/data without any processing
	str.WriteString("{\"Command\":\"")
	str.WriteString(runscript)
	str.WriteString("\",\"CommandArgs\":{\"mode\":\"create\",\"jname\":\"")
	str.WriteString(Jname)
	str.WriteString("\"")
	str.WriteString(", \"emulator\":\"")
	str.WriteString(vm_Engine)
	str.WriteString("\"")

	// todo: filter for insecured param=val
	for i := 0; i < val.NumField(); i++ {
		valueField := val.Field(i)

		typeField := val.Type().Field(i)
		tag := typeField.Tag

		tmpval := fmt.Sprintf("%s", valueField.Interface())
//		fmt.Printf("param %s valtype: %s\n",tmpval, val.Type)

		if len(tmpval) == 0 {
			continue
		}
		if len(tmpval) > 1000 {
			fmt.Printf("Error: param val too long\n")
			continue
		}

		fmt.Printf("[%s]\n", valueField)

		if len(typeField.Name) > 30 {
			fmt.Printf("Error: param name too long\n")
			continue
		}

		jconf_param = strings.ToLower(typeField.Name)

		if strings.Compare(jconf_param, "jname") == 0 {
			continue
		}

		if !regexpParamName.MatchString(jconf_param) {
			fmt.Printf("Error: wrong paramname: [%s]\n", jconf_param)
			continue
		} else {
			fmt.Printf("paramname test passed: [%s]\n", jconf_param)
		}

		// validate unknown data values
		switch jconf_param {
			case "vm_os_type":
			case "vm_os_profile":
			case "type":
			case "imgsize":
			case "ram":
			case "cpus":
			case "pkglist":
			case "pubkey":
			case "host_hostname":
			default:
				if !regexpParamVal.MatchString(tmpval) {
					fmt.Printf("Error: wrong paramval for %s: [%s]\n", jconf_param, tmpval)
					continue
				}
		}

		fmt.Printf("jconf: %s,\tField Name: %s,\t Field Value: %v,\t Tag Value: %s\n", jconf_param, typeField.Name, valueField.Interface(), tag.Get("tag_name"))

		var buf string

		if strings.Compare(jconf_param, "cpus") == 0 {
			// https://stackoverflow.com/questions/18041334/convert-interface-to-int
			cpus := valueField.Interface().(int)
			buf = fmt.Sprintf(",\"%s\": \"%d\"", jconf_param, cpus)
		} else {
			buf = fmt.Sprintf(",\"%s\": \"%s\"", jconf_param, tmpval)
		}

		buf2 := fmt.Sprintf("%s ", tmpval)
		str.WriteString(buf)
		recomendation.WriteString(buf2)
	}

	str.WriteString(",\"host_hostname\": \"")

	if len(vm.Host_hostname) > 1 {
		str.WriteString(vm.Host_hostname)
	} else {
		str.WriteString(InstanceId)
	}

	str.WriteString("\"}}")
	fmt.Printf("CMD: [%s]\n", str.String())
	response := fmt.Sprintf("{ \"id\": \"%s\", \"cluster\": \"curl -H cid:%x %s/api/v1/cluster\", \"status\": \"curl -H cid:%x %s/api/v1/status/%s\", \"start\": \"curl -H cid:%x %s/api/v1/start/%s\", \"stop\": \"curl -H cid:%x %s/api/v1/stop/%s\", \"destroy\": \"curl -H cid:%x %s/api/v1/destroy/%s\" }", InstanceId, cid, server_url, cid, server_url, InstanceId, cid, server_url, InstanceId, cid, server_url, InstanceId, cid, server_url, InstanceId)

	if err != nil {
		http.Error(w, err.Error(), http.StatusMethodNotAllowed)
		return
	}

	SqliteDBPath := fmt.Sprintf("%s/%x/%s-vm.ssh", *dbDir, cid, Jname)
	fmt.Printf("[debug] Create empty/mock status file: [%s]\n", SqliteDBPath)

	tfile, fileErr := os.Create(SqliteDBPath)
	if fileErr != nil {
		fmt.Println(fileErr)
		return
	}
	fmt.Fprintf(tfile, "{\n  \"id\": \"%s\",\n  \"is_power_on\": \"false\",\n  \"status\": \"pending\",\n  \"progress\": 0\n}\n", InstanceId)
	tfile.Close()

	getNodeRecomendation(recomendation.String(), suggest)

	// error code
	go realInstanceCreate(str.String())

	mapfile := fmt.Sprintf("%s/var/db/api/map/%x-%s", workdir, cid, InstanceId)
	m, err := os.Create(mapfile)

	if err != nil {
		log.Fatal(err)
	}

	_, err3 := m.WriteString(Jname)

	if err3 != nil {
		log.Fatal(err3)
	}

	m.Close()

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// write header is mandatory to overwrite header
	w.WriteHeader(200)
	fmt.Fprintln(w, response)

	return
}


func (feeds *MyFeeds) HandleClusterCreate(w http.ResponseWriter, r *http.Request) {

	var InstanceId string
	params := mux.Vars(r)

	var regexpVmOsType = regexp.MustCompile(`^[a-z_]+$`)
//	var regexpVmOsProfile = regexp.MustCompile(`^[aA-zZ0-9_\-\.]+$`)

	fmt.Println("create wakeup")

	InstanceId = params["InstanceId"]
	if !validateInstanceId(InstanceId) {
		JSONError(w, "The InstanceId should be valid form: ^[a-z_]([a-z0-9_])*$ (maxlen: 40)", http.StatusMethodNotAllowed)
		return
	}

	var regexpPubkey = regexp.MustCompile("^(ssh-rsa|ssh-dss|ssh-ed25519|ecdsa-[^ ]+) ([^ ]+) ?(.*)")

	if r.Body == nil {
		JSONError(w, "please send a request body", http.StatusMethodNotAllowed)
		return
	}

	fmt.Println("create wakeup2")

	var vm Vm

	body, err := ioutil.ReadAll(r.Body)

	if err != nil {
		log.Printf("ioutil readall body error %v", err)
		// handle net.Error...
		return
	}

	if err := json.Unmarshal(body, &vm); err != nil {
		errMsg := fmt.Sprintf("unmarsahal  error: %v", err)
		JSONError(w, errMsg, http.StatusMethodNotAllowed)
		log.Printf("unmarsahal to &vm error %v", err)
		return
	}

	switch vm.Vm_os_type {
		case "":
		default:
			if !regexpVmOsType.MatchString(vm.Vm_os_type) {
//				JSONError(w, "The Vm_os_type should be valid form: ^[aA-zZ0-9_\-\.]*$ (maxlen: 40)", http.StatusMethodNotAllowed)
				JSONError(w, "The Vm_os_type should be valid form: ^[aA-zZ0-9_-.]*$ (maxlen: 40)", http.StatusMethodNotAllowed)
				fmt.Printf("Error: Vm_os_type paramname: [%s]\n", vm.Vm_os_type)
				return
			} else {
				fmt.Printf("paramname test passed: [%s]\n", vm.Vm_os_type)
			}

			fmt.Printf("VM VM_OS_TYPE set: [%s]\n", vm.Vm_os_type)
			vm.Image=*vmEngine
	}
	switch vm.Vm_os_profile {
		case "":
		default:
			fmt.Printf("VM VM_OS_PROFILE set: [%s]\n", vm.Vm_os_profile)
			vm.Image=*vmEngine
	}

	switch vm.Image {
	case "":
		fmt.Println("Empty image field")
		JSONError(w, "Empty image field", http.StatusMethodNotAllowed)
		return
	case "jail":
		fmt.Printf("JAIL TYPE by img: [%s]\n", vm.Image)
	case "k8s":
		fmt.Printf("K8S TYPE by img: [%s]\n", vm.Image)
	default:
		fmt.Printf("VM TYPE by img: [%s]\n", vm.Image)
	}

	if len(vm.Pubkey) < 30 {
		fmt.Printf("Error: Pubkey too small: [%s]\n",vm.Pubkey)
		JSONError(w, "Pubkey too small", http.StatusMethodNotAllowed)
		return
	}

	if len(vm.Pubkey) > 1000 {
		fmt.Printf("Error: Pubkey too long\n")
		JSONError(w, "Pubkey too long", http.StatusMethodNotAllowed)
		return
	}

	if !regexpPubkey.MatchString(vm.Pubkey) {
		fmt.Printf("Error: pubkey should be valid form. valid key: ssh-rsa,ssh-ed25519,ecdsa-*,ssh-dsa XXXXX comment\n")
		JSONError(w, "pubkey should be valid form. valid key: ssh-rsa,ssh-ed25519,ecdsa-*,ssh-dsa XXXXX comment", http.StatusMethodNotAllowed)
		return
	}

	parsedKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(vm.Pubkey))
	if err != nil {

		fmt.Printf("Error: ParseAuthorizedKey\n")
		JSONError(w, "ParseAuthorizedKey", http.StatusMethodNotAllowed)
		return
	}

	fmt.Printf("pubKey: [%x]\n", parsedKey)

	if !isPubKeyAllowed(feeds, vm.Pubkey) {
		fmt.Printf("Pubkey not in ACL: %s\n", vm.Pubkey)
		JSONError(w, "not allowed", http.StatusMethodNotAllowed)
		return
	}

	uid := []byte(vm.Pubkey)
	Cid := md5.Sum(uid)

//	VmPathDir := fmt.Sprintf("%s/%x", *dbDir, cid)

//var totalinf interface{}

//	var v interface{}
//json.Unmarshal(jsonData, &v)
//data := v.(map[string]interface{})

	// auto-naming
	if InstanceId[0] == '_' {
		//sCid := string(Cid[:])
		sCid := fmt.Sprintf("%x", Cid)

		InstanceId = getId(sCid)
		if len(InstanceId) < 1 {
			fmt.Printf("Unable to get ID for CID: %s\n", sCid, vm.Pubkey)
			JSONError(w, "Unable to get ID", http.StatusMethodNotAllowed)
			return
		}

		fmt.Printf("GET NEXT FREE Id for [%s]: [%s]\n",sCid,InstanceId)
	}

	// route to subfunctim
	switch vm.Image {
	case "jail":
		runscript = *runScriptJail
		fmt.Printf("JAIL TYPE by img: [%s]\n", vm.Image)
		vm.Jname = InstanceId
		HandleCreateVm(w,vm);
	case "k8s":
		runscript = *runScriptK8s
		var cluster Cluster
		if err := json.Unmarshal(body, &cluster); err != nil {
			log.Printf("unmarsahal to &cluster error %v", err)
			return
		}
		cluster.K8s_name = InstanceId
		HandleCreateK8s(w,cluster);
	default:
		runscript = *runScriptVm
		fmt.Printf("VM TYPE by img: [%s]\n", vm.Image)
		vm.Jname = InstanceId
		HandleCreateVm(w,vm);
	}

	return
}


func dump(items []interface{}) {
	fmt.Println("Name")
	for i := 0; i < len(items); i++ {
		v := reflect.ValueOf(items[i])
		name := v.FieldByName("Name")
		fmt.Println(name.String())
	}
}

func somethingWentWrong(w http.ResponseWriter) {
	w.WriteHeader(500)
	w.Write([]byte("something went wrong"))
}


func (feeds *MyFeeds) HandleIac(w http.ResponseWriter, r *http.Request) {
	var InstanceId string
	var yaml string
	params := mux.Vars(r)

	fmt.Println("create wakeup")

	InstanceId = params["InstanceId"]
	if !validateInstanceId(InstanceId) {
		JSONError(w, "The InstanceId should be valid form: ^[a-z_]([a-z0-9_])*$ (maxlen: 40)", http.StatusMethodNotAllowed)
		return
	}

	if r.Body == nil {
		JSONError(w, "please send a request body", http.StatusMethodNotAllowed)
		return
	}
	//If its not multipart, We will expect file data in body.
//	if !strings.Contains(r.Header.Get("Content-Type"), "multipart/form-data") {
//		log.Println("multipart/form-data error")
//		//handleFileInBody(w, r)
//		return
//	}

/*
	checkMapfile := fmt.Sprintf("/tmp/iac.yaml")

	f, err := os.Create(checkMapfile)

	written, err := io.Copy(f, r.Body)
	if err != nil {
		log.Println("copy error", err)
//		somethingWentWrong(w)
		return
	}

	log.Println("Written", written)
*/

	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	//log.Println("TEST")

	// 32 MB is the default used by FormFile
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// get a reference to the fileHeaders
	files := r.MultipartForm.File["file"]

	for _, fileHeader := range files {
		if fileHeader.Size > MAX_UPLOAD_SIZE {
			http.Error(w, fmt.Sprintf("The uploaded image is too big: %s. Please use an image less than 1MB in size", fileHeader.Filename), http.StatusBadRequest)
			return
		}

		file, err := fileHeader.Open()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		defer file.Close()

		buff := make([]byte, 512)
	//	buff := make([]byte, 8)
		_, err = file.Read(buff)
		if err != nil {
			log.Println("Error file.Read buff ")
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		filetype := http.DetectContentType(buff)
	//	if filetype != "image/jpeg" && filetype != "image/png" {
		log.Println("Content Type: ", filetype)

		_, err = file.Seek(0, io.SeekStart)
		if err != nil {
		log.Println("Seek error")
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if !fileExists("/var/spool/cbsd-mq-api/upload") {
			fmt.Printf("* create spool dir: /var/spool/cbsd-mq-api/upload\n")
			err = os.MkdirAll("/var/spool/cbsd-mq-api/upload", os.ModePerm)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}

		yaml = fmt.Sprintf("%d.yaml", time.Now().UnixNano())

		// inherit extension
	//	f, err := os.Create(fmt.Sprintf("/var/spool/cbsd-mq-api/upload/%d%s.yaml", time.Now().UnixNano(), filepath.Ext(fileHeader.Filename)))
		f, err := os.Create(fmt.Sprintf("/var/spool/cbsd-mq-api/upload/%s", yaml))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		defer f.Close()

		pr := &Progress{
			TotalSize: fileHeader.Size,
		}

		_, err = io.Copy(f, io.TeeReader(file, pr))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}

	fmt.Fprintf(w, "Upload successful")

	go applyIac(InstanceId, yaml)

	return
}


func HandleCreateK8s(w http.ResponseWriter, cluster Cluster) {

	var InstanceId string
//	params := mux.Vars(r)
	var CurrentQueue int

	InstanceId = cluster.K8s_name

	// Check for global limt
	ClusterQueuePath := fmt.Sprintf("%s/queue", *k8sDbDir)
	if fileExists(ClusterQueuePath) {
		fd, err := os.Open(ClusterQueuePath)
		if err != nil {
			fmt.Printf("unable to read current queue len from %s\n", ClusterQueuePath)
			JSONError(w, "limits exceeded, please try again later", http.StatusMethodNotAllowed)
			return
		}
		defer fd.Close()

		_, err = fmt.Fscanf(fd, "%d", &CurrentQueue)
		if err != nil {
			if err != io.EOF {
				//log.Fatal(err)
				fmt.Printf("unable to read jname from %s\n", ClusterQueuePath)
				JSONError(w, "limits exceeded, please try again later", http.StatusMethodNotAllowed)
				return
			}
		}

		fmt.Printf("Current QUEUE: [%d]\n", CurrentQueue)
		if CurrentQueue >= clusterLimitMax {
			fmt.Printf("limits exceeded: (%d max)\n", clusterLimitMax)
			JSONError(w, "limits exceeded, please try again later", http.StatusMethodNotAllowed)
			return
		}
	}

	if !validateInstanceId(InstanceId) {
		JSONError(w, "The InstanceId should be valid form: ^[a-z_]([a-z0-9_])*$ (maxlen: 40)", http.StatusMethodNotAllowed)
		return
	}

	var regexpSize = regexp.MustCompile(`^[1-9](([0-9]+)?)([m|g|t])$`)
	var regexpEmail = regexp.MustCompile("^[a-zA-Z0-9.!#$%&'*+/=?^_`{|}~-]+@[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(?:\\.[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$")
	var regexpCallback = regexp.MustCompile(`^(http|https)://`)
	var regexpPubkey = regexp.MustCompile("^(ssh-rsa|ssh-dss|ssh-ed25519|ecdsa-[^ ]+) ([^ ]+) ?(.*)")
	var regexpParamName = regexp.MustCompile(`^[a-z_]+$`)
	var regexpParamVal = regexp.MustCompile(`^[aA-zZ0-9_\-. ]+$`)
	var regexpHostName = regexp.MustCompile(`^[aA-zZ0-9_\-\.]+$`)

	fmt.Println("create wakeup")

	var suggest string

	if len(cluster.Pubkey) < 30 {
		fmt.Printf("Error: Pubkey data too small: [%s]\n", cluster.Pubkey)
		JSONError(w, "Pubkey too small", http.StatusMethodNotAllowed)
		return
	}

	if len(cluster.Pubkey) > 1000 {
		fmt.Printf("Error: Pubkey too long\n")
		JSONError(w, "Pubkey too long", http.StatusMethodNotAllowed)
		return
	}

	if !regexpPubkey.MatchString(cluster.Pubkey) {
		fmt.Printf("Error: pubkey should be valid form. valid key: ssh-rsa,ssh-ed25519,ecdsa-*,ssh-dsa XXXXX comment\n")
		JSONError(w, "pubkey should be valid form. valid key: ssh-rsa,ssh-ed25519,ecdsa-*,ssh-dsa XXXXX comment", http.StatusMethodNotAllowed)
		return
	}

	parsedKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(cluster.Pubkey))
	if err != nil {
		fmt.Printf("Error: ParseAuthorizedKey\n")
		JSONError(w, "ParseAuthorizedKey", http.StatusMethodNotAllowed)
		return
	}

	fmt.Printf("pubKey: [%x]\n", parsedKey)
	uid := []byte(cluster.Pubkey)

	//existance?
	// check for existance
	cid := md5.Sum(uid)

//	if !isPubKeyAllowed(feeds, cluster.Pubkey) {
//		fmt.Printf("Pubkey not in ACL: %s\n", cluster.Pubkey)
//		JSONError(w, "not allowed", http.StatusMethodNotAllowed)
//		return
//	}

	// Count+Limits per CID should be implemented here (database req).
	ClusterTimePath := fmt.Sprintf("%s/%x.time", *k8sDbDir, cid)

	//!! FCP trial ONLY !!
	//if fileExists(ClusterTimePath) {
	//	fmt.Printf("Error: limit of clusters per user has been exceeded: [%s]\n", ClusterTimePath)
	//	JSONError(w, "limit of clusters per user has been exceeded: 1", http.StatusMethodNotAllowed)
	//	return
	//}

	ClusterTime := time.Now().Unix()

	tfile, fileErr := os.Create(ClusterTimePath)
	if fileErr != nil {
		fmt.Println(fileErr)
		return
	}
	fmt.Fprintf(tfile, "%s\n%s\n", ClusterTime, InstanceId)

	tfile.Close()

	ClusterPathDir := fmt.Sprintf("%s/%x", *k8sDbDir, cid)

	if !fileExists(ClusterPathDir) {
		os.Mkdir(ClusterPathDir, 0775)
	}

	ClusterPath := fmt.Sprintf("%s/%x/cluster-%s", *k8sDbDir, cid, InstanceId)

	if fileExists(ClusterPath) {
		fmt.Printf("Error: cluster already exist: [%s]\n", ClusterPath)
		JSONError(w, "cluster already exist", http.StatusMethodNotAllowed)
		return
	}

	fmt.Printf("cluster file not exist, create empty: [%s]\n", ClusterPath)
	// create empty file
	f, err := os.Create(ClusterPath)

	if err != nil {
		log.Fatal(err)
	}

	if len(cluster.Recomendation) > 1 {
		if !regexpHostName.MatchString(cluster.Recomendation) {
			fmt.Printf("Error: wrong hostname recomendation: [%s]\n", cluster.Recomendation)
			JSONError(w, "recomendation should be valid form. valid form", http.StatusMethodNotAllowed)
			return
		} else {
			fmt.Printf("Found cluster recomendation: [%s]\n", cluster.Recomendation)
			suggest = cluster.Recomendation
		}
	} else {
		suggest = ""
	}

	if len(cluster.Email) > 2 {
		if !regexpEmail.MatchString(cluster.Email) {
			response := Response{"email should be valid form"}
			js, err := json.Marshal(response)
			if err != nil {
				http.Error(w, err.Error(), http.StatusMethodNotAllowed)
				return
			}
			http.Error(w, string(js), 400)
			return
		}
	}

	if len(cluster.Callback) > 2 {
		if !regexpCallback.MatchString(cluster.Callback) {
			response := Response{"callback should be valid form"}
			js, err := json.Marshal(response)
			if err != nil {
				http.Error(w, err.Error(), http.StatusMethodNotAllowed)
				return
			}
			http.Error(w, string(js), 400)
			return
		}
	}

	Jname := getJname()
	if len(Jname) < 1 {
		log.Fatal("unable to get jname")
		return
	}

	fmt.Printf("GET NEXT FREE JNAME: [%s]\n", Jname)

	_, err2 := f.WriteString(Jname)

	if err2 != nil {
		log.Fatal(err2)
	}

	f.Close()

	// master value validation
	init_masters, err := strconv.Atoi(cluster.Init_masters)
	if err != nil {
		response := Response{"Init_masters not a number"}
		js, err := json.Marshal(response)
		if err != nil {
			http.Error(w, err.Error(), http.StatusMethodNotAllowed)
			return
		}
		http.Error(w, string(js), 400)
		return
	}
	if init_masters <= 0 || init_masters > 10 {
		response := Response{"Init_masters valid range: 1-10"}
		js, err := json.Marshal(response)
		if err != nil {
			http.Error(w, err.Error(), http.StatusMethodNotAllowed)
			return
		}
		http.Error(w, string(js), 400)
		return
	}

	if !regexpSize.MatchString(cluster.Master_vm_ram) {
		response := Response{"The master_vm_ram should be valid form, 512m, 1g"}
		js, err := json.Marshal(response)
		if err != nil {
			http.Error(w, err.Error(), http.StatusMethodNotAllowed)
			return
		}
		http.Error(w, string(js), 400)
		return
	}
	if !regexpSize.MatchString(cluster.Master_vm_imgsize) {
		response := Response{"The master_vm_imgsize should be valid form, 2g, 30g"}
		js, err := json.Marshal(response)
		if err != nil {
			http.Error(w, err.Error(), http.StatusMethodNotAllowed)
			return
		}
		http.Error(w, string(js), 400)
		return
	}

	var init_workers int

	// worker value valudation
	if len(cluster.Init_workers) > 1 {
		init_workers, err = strconv.Atoi(cluster.Init_workers)
		if err != nil {
			response := Response{"Init_workers not a number"}
			js, err := json.Marshal(response)
			if err != nil {
				http.Error(w, err.Error(), http.StatusMethodNotAllowed)
				return
			}
			http.Error(w, string(js), 400)
			return
		}
	} else {
		init_workers = 0
	}
	if init_workers < 0 || init_workers > 10 {
		response := Response{"Init_workers valid range: 0-10"}
		js, err := json.Marshal(response)
		if err != nil {
			http.Error(w, err.Error(), http.StatusMethodNotAllowed)
			return
		}
		http.Error(w, string(js), 400)
		return
	}
	if init_workers > 0 {
		if !regexpSize.MatchString(cluster.Worker_vm_ram) {
			response := Response{"The workers_vm_ram should be valid form, 512m, 1g"}
			js, err := json.Marshal(response)
			if err != nil {
				http.Error(w, err.Error(), http.StatusMethodNotAllowed)
				return
			}
			http.Error(w, string(js), 400)
			return
		}
		if !regexpSize.MatchString(cluster.Worker_vm_imgsize) {
			response := Response{"The worker_vm_imgsize should be valid form, 2g, 30g"}
			js, err := json.Marshal(response)
			if err != nil {
				http.Error(w, err.Error(), http.StatusMethodNotAllowed)
				return
			}
			http.Error(w, string(js), 400)
			return
		}
	}

	var pv_enable int

	// pv_enable value validation
	if len(cluster.Pv_enable) > 1 {
		pv_enable, err = strconv.Atoi(cluster.Pv_enable)
		if err != nil {
			response := Response{"Pv_enable not a number"}
			js, err := json.Marshal(response)
			if err != nil {
				http.Error(w, err.Error(), http.StatusMethodNotAllowed)
				return
			}
			http.Error(w, string(js), 400)
			return
		}
	} else {
		pv_enable = 1
	}
	if pv_enable < 0 || pv_enable > 1 {
		response := Response{"Pv_enable valid values: 0 or 1"}
		js, err := json.Marshal(response)
		if err != nil {
			http.Error(w, err.Error(), http.StatusMethodNotAllowed)
			return
		}
		http.Error(w, string(js), 400)
		return
	}

	var kubelet_master int

	if len(cluster.Kubelet_master) > 1 {
		// pv_enable value validation
		kubelet_master, err = strconv.Atoi(cluster.Kubelet_master)
		if err != nil {
			response := Response{"Kubelet_master not a number"}
			js, err := json.Marshal(response)
			if err != nil {
				http.Error(w, err.Error(), http.StatusMethodNotAllowed)
				return
			}
			http.Error(w, string(js), 400)
			return
		}
	} else {
		kubelet_master = 1
	}
	if kubelet_master < 0 || kubelet_master > 1 {
		response := Response{"Kubelet_master valid values: 0 or 1"}
		js, err := json.Marshal(response)
		if err != nil {
			http.Error(w, err.Error(), http.StatusMethodNotAllowed)
			return
		}
		http.Error(w, string(js), 400)
		return
	}

	cluster.K8s_name = InstanceId
	val := reflect.ValueOf(cluster)

	var jconf_param string
	var str strings.Builder
	var recomendation strings.Builder

	// of course we can use marshal here instead of string concatenation,
	// but now this is too simple case/data without any processing
	str.WriteString("{\"Command\":\"")
	str.WriteString(*runScriptK8s)
	str.WriteString("\",\"CommandArgs\":{\"mode\":\"init\",\"k8s_name\":\"")
	//	str.WriteString(InstanceId)
	str.WriteString(Jname)
	str.WriteString("\"")

	for i := 0; i < val.NumField(); i++ {
		valueField := val.Field(i)

		typeField := val.Type().Field(i)
		tag := typeField.Tag

		tmpval := fmt.Sprintf("%s", valueField.Interface())

		if len(tmpval) == 0 {
			continue
		}
		if len(tmpval) > 1000 {
			fmt.Printf("Error: param val too long\n")
			continue
		}

		fmt.Printf("[%s]", valueField)

		if len(typeField.Name) > 30 {
			fmt.Printf("Error: param name too long\n")
			continue
		}

		jconf_param = strings.ToLower(typeField.Name)

		if strings.Compare(jconf_param, "jname") == 0 {
			continue
		}

		if !regexpParamName.MatchString(jconf_param) {
			fmt.Printf("Error: wrong paramname: [%s]\n", jconf_param)
			continue
		} else {
			fmt.Printf("paramname test passed: [%s]\n", jconf_param)
		}

		// validate unknown data values
		switch jconf_param {
			case "type":
			case "imgsize":
			case "ram":
			case "cpus":
			case "pkglist":
			case "pubkey":
			case "host_hostname":
			case "init_masters":
			case "init_workers":
			case "master_vm_ram":
			case "master_vm_cpus":
			case "master_vm_imgsize":
			case "worker_vm_ram":
			case "worker_vm_cpus":
			case "worker_vm_imgsize":
			case "pv_enable":
			case "kubelet_master":
			case "email":
			case "callback":
			default:
				if !regexpParamVal.MatchString(tmpval) {
					fmt.Printf("Error: wrong paramval for %s: [%s]\n", jconf_param, tmpval)
					continue
				}
		}

		fmt.Printf("jconf: %s,\tField Name: %s,\t Field Value: %v,\t Tag Value: %s\n", jconf_param, typeField.Name, valueField.Interface(), tag.Get("tag_name"))

		var buf string

		if strings.Compare(jconf_param, "cpus") == 0 {
			// https://stackoverflow.com/questions/18041334/convert-interface-to-int
			cpus := valueField.Interface().(int)
			buf = fmt.Sprintf(",\"%s\": \"%d\"", jconf_param, cpus)
		} else {
			buf = fmt.Sprintf(",\"%s\": \"%s\"", jconf_param, tmpval)
		}

		buf2 := fmt.Sprintf("%s ", tmpval)
		str.WriteString(buf)
		recomendation.WriteString(buf2)
	}

	str.WriteString("}}")
	fmt.Printf("C: [%s]\n", str.String())
	response := fmt.Sprintf("{ \"Message\": [\"curl -H cid:%x %s/api/v1/cluster\", \"curl -H cid:%x %s/api/v1/status/%s\", \"curl -H cid:%x %s/api/v1/kubeconfig/%s\",  \"curl -H cid:%x %s/api/v1/snapshot/%s\", \"curl -H cid:%x %s/api/v1/rollback/%s\", \"curl -H cid:%x %s/api/v1/destroy/%s\"] }", cid, server_url, cid, server_url, InstanceId, cid, server_url, InstanceId, cid, server_url, InstanceId, cid, server_url, InstanceId, cid, server_url, InstanceId)

	getNodeRecomendation(recomendation.String(), suggest)

	// mock status
	SqliteDBPath := fmt.Sprintf("%s/%x/%s-vm.ssh", *k8sDbDir, cid, Jname)
	fmt.Printf("Create empty/mock status file: [%s]\n", SqliteDBPath)

	tfile, fileErr = os.Create(SqliteDBPath)
	if fileErr != nil {
		fmt.Println(fileErr)
		return
	}

	fmt.Fprintf(tfile, "{\n  \"id\": \"%s\",\n  \"is_power_on\": \"false\",\n  \"status\": \"pending\",\n  \"progress\": 0\n}\n", InstanceId)

	tfile.Close()

	go realInstanceCreate(str.String())

	// !!! MKDIR
	ClusterMapDir := fmt.Sprintf("%s/var/db/k8s/map", workdir)

	if !fileExists(ClusterMapDir) {
		os.Mkdir(ClusterMapDir, 0775)
	}

	mapfile := fmt.Sprintf("%s/%x-%s", ClusterMapDir, cid, InstanceId)
	m, err := os.Create(mapfile)

	if err != nil {
		log.Fatal(err)
	}

	_, err3 := m.WriteString(Jname)

	if err3 != nil {
		log.Fatal(err3)
	}

	m.Close()

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// write header is mandatory to overwrite header
	w.WriteHeader(200)
	fmt.Fprintln(w, response)

	return
}

func (feeds *MyFeeds) HandleClusterDestroy(w http.ResponseWriter, r *http.Request) {
	var InstanceId string
	params := mux.Vars(r)
	// enum { 0 - vm, 1 - k8s }
	var vmType int

	InstanceId = params["InstanceId"]

	if !validateInstanceId(InstanceId) {
		JSONError(w, "The InstanceId should be valid form: ^[a-z_]([a-z0-9_])*$ (maxlen: 40)", http.StatusMethodNotAllowed)
		return
	}

	Cid := r.Header.Get("cid")
	if !validateCid(Cid) {
		JSONError(w, "The cid should be valid form: ^[a-f0-9]{32}$", http.StatusMethodNotAllowed)
		return
	}

	if !isCidAllowed(feeds, Cid) {
		fmt.Printf("CID not in ACL: %s\n", Cid)
		JSONError(w, "not allowed", http.StatusMethodNotAllowed)
		return
	}

	var mapfile string

	checkMapfile := fmt.Sprintf("%s/var/db/api/map/%s-%s", workdir, Cid, InstanceId)
	if _, err := os.Stat(checkMapfile); os.IsNotExist(err) {
		fmt.Printf("status: no such %s/%s/vms - check K8S...\n", *dbDir, Cid)
		// check K8S dir
		checkMapfile = fmt.Sprintf("%s/var/db/k8s/map/%s-%s", workdir, Cid, InstanceId)
		if _, err := os.Stat(checkMapfile); os.IsNotExist(err) {
			JSONError(w, "not found", http.StatusOK)
			return
		} else {
			fmt.Printf("%s found - its K8S\n", checkMapfile)
			// K8S instance
			vmType = 1
			mapfile = checkMapfile
		}
	} else {
		//VM/jail instance
		fmt.Printf("%s/%s/vms found - its not K8S\n", *dbDir, Cid)
		vmType = 0
		mapfile = checkMapfile
	}

	b, err := ioutil.ReadFile(mapfile) // just pass the file name
	if err != nil {
		fmt.Printf("unable to read jname from map file: [%s]\n", mapfile)
		JSONError(w, "not found", http.StatusOK)
		return
	}

	fmt.Printf("Destroy %s via map file: %s\n", string(b), mapfile)

	// of course we can use marshal here instead of string concatenation,
	// but now this is too simple case/data without any processing
	var str strings.Builder
	var SqliteDBPath string

	// destroy via
	if ( vmType == 1 ) {
		// K8s
		SqliteDBPath = fmt.Sprintf("%s/%s/%s.node", *k8sDbDir, Cid, string(b))
		runscript = *destroyK8sScript
	} else {
		SqliteDBPath = fmt.Sprintf("%s/%s/%s.node", *dbDir, Cid, string(b))
		runscript = *destroyScript
	}
	str.WriteString("{\"Command\":\"")
	str.WriteString(runscript)

	if ( vmType == 1 ) {
		// K8s
		str.WriteString("\",\"CommandArgs\":{\"mode\":\"destroy\",\"k8s_name\":\"")
	} else {
		str.WriteString("\",\"CommandArgs\":{\"mode\":\"destroy\",\"jname\":\"")
	}

	str.WriteString(string(b))
	str.WriteString("\"")
	str.WriteString("}}")

	//get guest nodes & tubes
	if fileExists(SqliteDBPath) {
		b, err := ioutil.ReadFile(SqliteDBPath) // just pass the file name
		if err != nil {
			fmt.Printf("unable to read node map: %s\n", SqliteDBPath)
			JSONError(w, "unable to read node map", http.StatusOK)
			return
		} else {
			result := strings.Replace(string(b), ".", "_", -1)
			result = strings.Replace(result, "-", "_", -1)
			result = strings.TrimSuffix(result, "\n")
			tube := fmt.Sprintf("cbsd_%s", result)
			reply := fmt.Sprintf("cbsd_%s_result_id", result)
			// result: srv-03.olevole.ru
			config.BeanstalkConfig.Tube = tube
			config.BeanstalkConfig.ReplyTubePrefix = reply
		}
	} else {
		fmt.Printf("unable to read node map: %s\n", SqliteDBPath)
		JSONError(w, "unable to read node map", http.StatusOK)
		return
	}

	fmt.Printf("C: [%s]\n", str.String())
	go realInstanceCreate(str.String())

	e := os.Remove(mapfile)
	if e != nil {
		log.Fatal(e)
	}

	// remove from FS
	var VmPath string
	if ( vmType == 1 ) {
		// K8S
		VmPath = fmt.Sprintf("%s/%s/cluster-%s", *k8sDbDir, Cid, InstanceId)
		if fileExists(VmPath) {
			b, err := ioutil.ReadFile(VmPath) // just pass the file name
			if err != nil {
				fmt.Printf("Error read UID from  [%s]\n", string(b))
			} else {

				fmt.Printf("   REMOVE: %s\n", VmPath)
				e = os.Remove(VmPath)

				VmPath = fmt.Sprintf("%s/%s/%s.node", *k8sDbDir, Cid, string(b))
				fmt.Printf("   REMOVE: %s\n", VmPath)
				e = os.Remove(VmPath)

				VmPath = fmt.Sprintf("%s/%s/%s-vm.ssh", *k8sDbDir, Cid, string(b))
				fmt.Printf("   REMOVE: %s\n", VmPath)
				e = os.Remove(VmPath)

				VmPath = fmt.Sprintf("%s/%s/vms/%s", *k8sDbDir, Cid, string(b))
				fmt.Printf("   REMOVE: %s\n", VmPath)
				e = os.Remove(VmPath)
			}
		}
	} else {
		// VM
		VmPath = fmt.Sprintf("%s/%s/vm-%s", *dbDir, Cid, InstanceId)
		if fileExists(VmPath) {
			b, err := ioutil.ReadFile(VmPath) // just pass the file name
			if err != nil {
				fmt.Printf("Error read UID from  [%s]\n", string(b))
			} else {

				fmt.Printf("   REMOVE: %s\n", VmPath)
				e = os.Remove(VmPath)

				VmPath = fmt.Sprintf("%s/%s/%s.node", *dbDir, Cid, string(b))
				fmt.Printf("   REMOVE: %s\n", VmPath)
				e = os.Remove(VmPath)

				VmPath = fmt.Sprintf("%s/%s/%s-vm.ssh", *dbDir, Cid, string(b))
				fmt.Printf("   REMOVE: %s\n", VmPath)
				e = os.Remove(VmPath)

				VmPath = fmt.Sprintf("%s/%s/vms/%s", *dbDir, Cid, string(b))
				fmt.Printf("   REMOVE: %s\n", VmPath)
				e = os.Remove(VmPath)
			}
		}
	}

	JSONError(w, "destroy", 200)
	return
}

func (feeds *MyFeeds) HandleClusterStop(w http.ResponseWriter, r *http.Request) {
	var InstanceId string
	params := mux.Vars(r)

	InstanceId = params["InstanceId"]
	if !validateInstanceId(InstanceId) {
		JSONError(w, "The InstanceId should be valid form: ^[a-z_]([a-z0-9_])*$ (maxlen: 40)", http.StatusMethodNotAllowed)
		return
	}

	Cid := r.Header.Get("cid")
	if !validateCid(Cid) {
		JSONError(w, "The cid should be valid form: ^[a-f0-9]{32}$", http.StatusMethodNotAllowed)
		return
	}

	if !isCidAllowed(feeds, Cid) {
		fmt.Printf("CID not in ACL: %s\n", Cid)
		JSONError(w, "not allowed", http.StatusMethodNotAllowed)
		return
	}

	HomePath := fmt.Sprintf("%s/%s/vms", *dbDir, Cid)
	if _, err := os.Stat(HomePath); os.IsNotExist(err) {
		return
	}

	mapfile := fmt.Sprintf("%s/var/db/api/map/%s-%s", workdir, Cid, InstanceId)

	if !fileExists(config.Recomendation) {
		fmt.Printf("no such map file %s/var/db/api/map/%s-%s\n", workdir, Cid, InstanceId)
		JSONError(w, "not found", http.StatusOK)
		return
	}

	b, err := ioutil.ReadFile(mapfile) // just pass the file name
	if err != nil {
		fmt.Printf("unable to read jname from %s/var/db/api/map/%s-%s\n", workdir, Cid, InstanceId)
		JSONError(w, "not found", http.StatusOK)
		return
	}

	fmt.Printf("stop %s via %s/var/db/api/map/%s-%s\n", string(b), workdir, Cid, InstanceId)

	// of course we can use marshal here instead of string concatenation,
	// but now this is too simple case/data without any processing
	var str strings.Builder

	runscript = *stopScript
	str.WriteString("{\"Command\":\"")
	str.WriteString(runscript)
	str.WriteString("\",\"CommandArgs\":{\"mode\":\"stop\",\"jname\":\"")
	str.WriteString(string(b))
	str.WriteString("\"")
	str.WriteString("}}")

	//get guest nodes & tubes
	SqliteDBPath := fmt.Sprintf("%s/%s/%s.node", *dbDir, Cid, string(b))
	if fileExists(SqliteDBPath) {
		b, err := ioutil.ReadFile(SqliteDBPath) // just pass the file name
		if err != nil {
			JSONError(w, "{}", 400)
			return
		} else {
			result := strings.Replace(string(b), ".", "_", -1)
			result = strings.Replace(result, "-", "_", -1)
			result = strings.TrimSuffix(result, "\n")
			//	result = strings.Replace(result, "\r\n", "", -1)

			tube := fmt.Sprintf("cbsd_%s", result)
			reply := fmt.Sprintf("cbsd_%s_result_id", result)

			fmt.Printf("Tube selected: [%s]\n", tube)
			fmt.Printf("ReplyTube selected: [%s]\n", reply)

			// result: srv-03.olevole.ru
			config.BeanstalkConfig.Tube = tube
			config.BeanstalkConfig.ReplyTubePrefix = reply
		}
	} else {
		JSONError(w, "nodes not found", http.StatusOK)
		return
	}

	fmt.Printf("C: [%s]\n", str.String())
	go realInstanceCreate(str.String())

	// remove from FS
	VmPath := fmt.Sprintf("%s/%s/vm-%s", *dbDir, Cid, InstanceId)
	if fileExists(VmPath) {
		b, err := ioutil.ReadFile(VmPath) // just pass the file name
		if err != nil {
			fmt.Printf("Error read UID from  [%s]\n", string(b))
		}
	}

	JSONError(w, "stopped", 200)
	return
}

func (feeds *MyFeeds) HandleClusterStart(w http.ResponseWriter, r *http.Request) {
	var InstanceId string
	params := mux.Vars(r)

	InstanceId = params["InstanceId"]
	if !validateInstanceId(InstanceId) {
		JSONError(w, "The InstanceId should be valid form: ^[a-z_]([a-z0-9_])*$ (maxlen: 40)", http.StatusMethodNotAllowed)
		return
	}

	Cid := r.Header.Get("cid")
	if !validateCid(Cid) {
		JSONError(w, "The cid should be valid form: ^[a-f0-9]{32}$", http.StatusMethodNotAllowed)
		return
	}

	if !isCidAllowed(feeds, Cid) {
		fmt.Printf("CID not in ACL: %s\n", Cid)
		JSONError(w, "not allowed", http.StatusMethodNotAllowed)
		return
	}

	HomePath := fmt.Sprintf("%s/%s/vms", *dbDir, Cid)
	if _, err := os.Stat(HomePath); os.IsNotExist(err) {
		return
	}

	w.Header().Set("Content-Type", "application/json")
	mapfile := fmt.Sprintf("%s/var/db/api/map/%s-%s", workdir, Cid, InstanceId)

	if !fileExists(config.Recomendation) {
		fmt.Printf("no such map file %s/var/db/api/map/%s-%s\n", workdir, Cid, InstanceId)
		JSONError(w, "not found", http.StatusOK)
		return
	}

	b, err := ioutil.ReadFile(mapfile) // just pass the file name
	if err != nil {
		fmt.Printf("unable to read jname from %s/var/db/api/map/%s-%s\n", workdir, Cid, InstanceId)
		JSONError(w, "not found", http.StatusOK)
		return
	}

	fmt.Printf("start %s via %s/var/db/api/map/%s-%s\n", string(b), workdir, Cid, InstanceId)

	// of course we can use marshal here instead of string concatenation,
	// but now this is too simple case/data without any processing
	var str strings.Builder

	runscript = *startScript
	str.WriteString("{\"Command\":\"")
	str.WriteString(runscript)
	str.WriteString("\",\"CommandArgs\":{\"mode\":\"start\",\"jname\":\"")
	str.WriteString(string(b))
	str.WriteString("\"")
	str.WriteString("}}")

	//get guest nodes & tubes
	SqliteDBPath := fmt.Sprintf("%s/%s/%s.node", *dbDir, Cid, string(b))
	if fileExists(SqliteDBPath) {
		b, err := ioutil.ReadFile(SqliteDBPath) // just pass the file name
		if err != nil {
			http.Error(w, "{}", 400)
			return
		} else {
			result := strings.Replace(string(b), ".", "_", -1)
			result = strings.Replace(result, "-", "_", -1)
			result = strings.TrimSuffix(result, "\n")
			//	result = strings.Replace(result, "\r\n", "", -1)

			tube := fmt.Sprintf("cbsd_%s", result)
			reply := fmt.Sprintf("cbsd_%s_result_id", result)

			fmt.Printf("Tube selected: [%s]\n", tube)
			fmt.Printf("ReplyTube selected: [%s]\n", reply)

			// result: srv-03.olevole.ru
			config.BeanstalkConfig.Tube = tube
			config.BeanstalkConfig.ReplyTubePrefix = reply
		}
	} else {
		JSONError(w, "nodes not found", http.StatusOK)
		return
	}

	fmt.Printf("C: [%s]\n", str.String())
	go realInstanceCreate(str.String())

	// remove from FS
	VmPath := fmt.Sprintf("%s/%s/vm-%s", *dbDir, Cid, InstanceId)
	if fileExists(VmPath) {
		b, err := ioutil.ReadFile(VmPath) // just pass the file name
		if err != nil {
			fmt.Printf("Error read UID from  [%s]\n", string(b))
		}
	}

	JSONError(w, "started", 200)
	return
}

func (feeds *MyFeeds) HandleIacRequestStatus(w http.ResponseWriter, r *http.Request) {
	var InstanceId string
	params := mux.Vars(r)

	InstanceId = params["InstanceId"]

	InstanceId = params["InstanceId"]
	if !validateInstanceId(InstanceId) {
		JSONError(w, "The InstanceId should be valid form: ^[a-z_]([a-z0-9_])*$ (maxlen: 40)", http.StatusMethodNotAllowed)
		return
	}

	progressFile := fmt.Sprintf("%s/%s.status",spool_Dir,InstanceId);

//      if r.Body == nil {
//              JSONError(w, "please send a request body", http.StatusInternalServerError)
//              return
//      }
	fmt.Printf("CHECK FOR: [%s]\n", progressFile)

	if !fileExists(progressFile) {
		fmt.Printf("Error: projectId not exist: [%s]\n", progressFile)
		JSONError(w, "projectId not exist", http.StatusNotFound)
		return
	}

	b, err := ioutil.ReadFile(progressFile) // just pass the file name
	if err != nil {
		fmt.Printf("unable to read progress file from: [%s]\n", progressFile)
		JSONError(w, "", 400)
		return
	}

	// already in json - send as-is
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.Error(w, string(b), 200)
	return
}

func (feeds *MyFeeds) HandleOneTimeConf(w http.ResponseWriter, r *http.Request) {
	var CfgFile string
	params := mux.Vars(r)

	CfgFile = params["CfgFile"]

	CfgFile = params["CfgFile"]
	if !validateCfgFile(CfgFile) {
		fmt.Printf("The CfgFile should be valid form: ^[a-z_]([a-z0-9_])*$ (maxlen: 10)", CfgFile)
		JSONError(w, "", 400)
		return
	}

	configFile := fmt.Sprintf("%s/%s",onetime_Dir,CfgFile);

	if !fileExists(configFile) {
		fmt.Printf("Error: no such CfgFile: [%s]\n", configFile)
		JSONError(w, "", 400)
		return
	}

	b, err := ioutil.ReadFile(configFile) // just pass the file name
	if err != nil {
		fmt.Printf("unable to read CfgFile: [%s]\n", configFile)
		JSONError(w, "", 400)
		return
	}

	e := os.Remove(configFile)
	if e != nil {
		fmt.Printf("unable to unlink CfgFile: [%s]\n", configFile)
	}

	// already in json - send as-is
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.Error(w, string(b), 200)


	return
}

