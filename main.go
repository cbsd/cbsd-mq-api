// CBSD Project 2013-2021
package main

import (
	"bufio"
	"crypto/md5"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/gorilla/mux"
	"golang.org/x/crypto/ssh"
)

var lock = sync.RWMutex{}
var config Config
var runscript string
var workdir string
var server_url string
var acl_enable bool

type Response struct {
	Message string
}

// The cluster Type. Name of elements must match with jconf params
type Vm struct {
	Type          string `json:type,omitempty"`
	Jname         string `json:jname,omitempty"`
	Img           string `json:img,omitempty"`
	Ram           string `json:ram,omitempty"`
	Cpus          string `"cpus,omitempty"`
	Imgsize       string `"imgsize,omitempty"`
	Pubkey        string `"pubkey,omitempty"`
	PkgList       string `"pkglist,omitempty"`
	Extras        string `"extras,omitempty"`
	Recomendation string `"recomendation,omitempty"`
	Host_hostname string `"host_hostname,omitempty"`
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
	runScriptBhyve         = flag.String("runscript_bhyve", "bhyve-api", "CBSD target run script")
	destroyScript          = flag.String("destroy_script", "control-api", "CBSD target run script")
	startScript            = flag.String("start_script", "control-api", "CBSD target run script")
	stopScript             = flag.String("stop_script", "control-api", "CBSD target run script")
	serverUrl              = flag.String("server_url", "http://127.0.0.1:65532", "Server URL for external requests")
	dbDir                  = flag.String("dbdir", "/var/db/cbsd-api", "db root dir")
	allowListFile          = flag.String("allowlist", "", "Path to PubKey whitelist, e.g: -allowlist /usr/local/etc/cbsd-mq-api.allow")
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
			fmt.Fprintln(w, "{\"Message\":\"Marshal error\"}", http.StatusInternalServerError)
			return
		}
		http.Error(w, string(js), code)
	} else {
		http.Error(w, "{}", http.StatusNotFound)
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

	f := &Feed{}

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
	router.HandleFunc("/api/v1/start/{InstanceId}", feeds.HandleClusterStart).Methods("GET")
	router.HandleFunc("/api/v1/stop/{InstanceId}", feeds.HandleClusterStop).Methods("GET")
	router.HandleFunc("/api/v1/cluster", feeds.HandleClusterCluster).Methods("GET")
	router.HandleFunc("/images", HandleClusterImages).Methods("GET")
	router.HandleFunc("/api/v1/destroy/{InstanceId}", feeds.HandleClusterDestroy).Methods("GET")
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

func validateVmType(VmType string) bool {
	var regexpVmType = regexp.MustCompile("^[a-z]+$")

	//current valid values:
	// 'jail', 'bhyve', 'xen'
	if len(VmType) < 2 || len(VmType) > 7 {
		return false
	}

	if regexpVmType.MatchString(VmType) {
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

//func HandleClusterStatus(w http.ResponseWriter, r *http.Request) {
func (feeds *MyFeeds) HandleClusterStatus(w http.ResponseWriter, r *http.Request) {
	var InstanceId string
	params := mux.Vars(r)

	InstanceId = params["InstanceId"]
	if !validateInstanceId(InstanceId) {
		JSONError(w, "The InstanceId should be valid form: ^[a-z_]([a-z0-9_])*$ (maxlen: 40)", http.StatusNotFound)
		return
	}

	Cid := r.Header.Get("cid")
	if !validateCid(Cid) {
		JSONError(w, "The cid should be valid form: ^[a-f0-9]{32}$", http.StatusNotFound)
		return
	}

	if !isCidAllowed(feeds, Cid) {
		fmt.Printf("CID not in ACL: %s\n", Cid)
		JSONError(w, "not allowed", http.StatusInternalServerError)
		return
	}

	HomePath := fmt.Sprintf("%s/%s/vms", *dbDir, Cid)
	if _, err := os.Stat(HomePath); os.IsNotExist(err) {
		JSONError(w, "not found", http.StatusNotFound)
		return
	}

	mapfile := fmt.Sprintf("%s/var/db/api/map/%s-%s", workdir, Cid, InstanceId)

	if !fileExists(config.Recomendation) {
		fmt.Printf("no such map file %s/var/db/api/map/map/%s-%s\n", workdir, Cid, InstanceId)
		JSONError(w, "not found", http.StatusNotFound)
		return
	}

	b, err := ioutil.ReadFile(mapfile) // just pass the file name
	if err != nil {
		fmt.Printf("unable to read jname from %s/var/db/api/map/%s-%s\n", workdir, Cid, InstanceId)
		JSONError(w, "not found", http.StatusNotFound)
		return
	}

	SqliteDBPath := fmt.Sprintf("%s/%s/%s-bhyve.ssh", *dbDir, Cid, string(b))
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
		JSONError(w, "", http.StatusNotFound)
	}
}

func (feeds *MyFeeds) HandleClusterCluster(w http.ResponseWriter, r *http.Request) {
	Cid := r.Header.Get("cid")
	if !validateCid(Cid) {
		JSONError(w, "The cid should be valid form: ^[a-f0-9]{32}$", http.StatusNotFound)
		return
	}

	if !isCidAllowed(feeds, Cid) {
		fmt.Printf("CID not in ACL: %s\n", Cid)
		JSONError(w, "not allowed", http.StatusInternalServerError)
		return
	}

	HomePath := fmt.Sprintf("%s/%s/vms", *dbDir, Cid)
	//fmt.Println("CID IS: [ %s ]", cid)
	if _, err := os.Stat(HomePath); os.IsNotExist(err) {
		JSONError(w, "", http.StatusNotFound)
		return
	}

	SqliteDBPath := fmt.Sprintf("%s/%s/vm.list", *dbDir, Cid)
	if fileExists(SqliteDBPath) {
		b, err := ioutil.ReadFile(SqliteDBPath) // just pass the file name
		if err != nil {
			JSONError(w, "", http.StatusNotFound)
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
		JSONError(w, "", http.StatusNotFound)
		return
	}
}

func HandleClusterImages(w http.ResponseWriter, r *http.Request) {

	if fileExists(config.Cloud_images_list) {
		b, err := ioutil.ReadFile(config.Cloud_images_list) // just pass the file name
		if err != nil {
			JSONError(w, "", http.StatusNotFound)
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
		JSONError(w, "", http.StatusNotFound)
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

//func (feeds *MyFeeds) HandleClusterCluster(w http.ResponseWriter, r *http.Request) {
//func HandleClusterCreate(w http.ResponseWriter, r *http.Request) {
func (feeds *MyFeeds) HandleClusterCreate(w http.ResponseWriter, r *http.Request) {

	var InstanceId string
	params := mux.Vars(r)

	InstanceId = params["InstanceId"]
	if !validateInstanceId(InstanceId) {
		JSONError(w, "The InstanceId should be valid form: ^[a-z_]([a-z0-9_])*$ (maxlen: 40)", http.StatusNotFound)
		return
	}

	var regexpPkgList = regexp.MustCompile(`^[aA-zZ_]([aA-zZ0-9_\-/ ])*$`)
	var regexpExtras = regexp.MustCompile("^[a-zA-Z0-9:,]*$")
	var regexpSize = regexp.MustCompile(`^[1-9](([0-9]+)?)([m|g|t])$`)
	var regexpPubkey = regexp.MustCompile("^(ssh-rsa|ssh-dss|ssh-ed25519|ecdsa-[^ ]+) ([^ ]+) ?(.*)")
	var regexpParamName = regexp.MustCompile(`^[a-z_]+$`)
	var regexpParamVal = regexp.MustCompile(`^[aA-zZ0-9_\-. ]+$`)
	var regexpHostName = regexp.MustCompile(`^[aA-zZ0-9_\-\.]+$`)

	var suggest string

	if r.Body == nil {
		JSONError(w, "please send a request body", http.StatusInternalServerError)
		return
	}

	fmt.Println("create wakeup")

	var vm Vm
	_ = json.NewDecoder(r.Body).Decode(&vm)

	if !validateVmType(vm.Type) {
		JSONError(w, "Unknown resource type, valid: 'bhyve', 'jail'", http.StatusInternalServerError)
		return
	}

	switch vm.Type {
	case "jail":
		fmt.Println(vm.Type, "type selected")
		runscript = *runScriptJail
	case "bhyve":
		fmt.Println(vm.Type, "type selected")
		runscript = *runScriptBhyve
	default:
		fmt.Println("Unknown resource type:", vm.Type, "valid: 'bhyve', 'jail'")
		JSONError(w, "Unknown resource type, valid: 'bhyve', 'jail'", http.StatusInternalServerError)
		return
	}

	if len(vm.Pubkey) < 30 {
		fmt.Printf("Error: Pubkey too small\n")
		JSONError(w, "Pubkey too small", http.StatusInternalServerError)
		return
	}

	if len(vm.Pubkey) > 1000 {
		fmt.Printf("Error: Pubkey too long\n")
		JSONError(w, "Pubkey too long", http.StatusInternalServerError)
		return
	}

	if !regexpPubkey.MatchString(vm.Pubkey) {
		fmt.Printf("Error: pubkey should be valid form. valid key: ssh-rsa,ssh-ed25519,ecdsa-*,ssh-dsa XXXXX comment\n")
		JSONError(w, "pubkey should be valid form. valid key: ssh-rsa,ssh-ed25519,ecdsa-*,ssh-dsa XXXXX comment", http.StatusInternalServerError)
		return
	}

	parsedKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(vm.Pubkey))
	if err != nil {

		fmt.Printf("Error: ParseAuthorizedKey\n")
		JSONError(w, "ParseAuthorizedKey", http.StatusInternalServerError)
		return
	}

	fmt.Printf("pubKey: [%x]\n", parsedKey)
	uid := []byte(vm.Pubkey)

	//existance?
	// check for existance
	cid := md5.Sum(uid)

	if !isPubKeyAllowed(feeds, vm.Pubkey) {
		fmt.Printf("Pubkey not in ACL: %s\n", vm.Pubkey)
		JSONError(w, "not allowed", http.StatusInternalServerError)
		return
	}

	VmPathDir := fmt.Sprintf("%s/%x", *dbDir, cid)

	if !fileExists(VmPathDir) {
		os.Mkdir(VmPathDir, 0775)
	}

	VmPath := fmt.Sprintf("%s/%x/vm-%s", *dbDir, cid, InstanceId)

	if fileExists(VmPath) {
		fmt.Printf("Error: vm already exist: [%s]\n", VmPath)
		JSONError(w, "vm already exist", http.StatusInternalServerError)
		return
	}

	fmt.Printf("vm file not exist, create empty: [%s]\n", VmPath)
	// create empty file
	f, err := os.Create(VmPath)

	if err != nil {
		log.Fatal(err)
	}

	if len(vm.PkgList) > 1 {
		if strings.Compare(vm.Type, "jail") == 0 {
			if !regexpPkgList.MatchString(vm.PkgList) {
				fmt.Printf("Error: wrong pkglist: [%s]\n", vm.PkgList)
				JSONError(w, "pkglist should be valid form. valid form", http.StatusInternalServerError)
				return
			}
		} else {
			fmt.Printf("Error: Pkglist for jail type only: [%s]\n", vm.Type)
			JSONError(w, "Pkglist for jail type only", http.StatusInternalServerError)
			return
		}
	}

	if len(vm.Host_hostname) > 1 {
		if !regexpHostName.MatchString(vm.Host_hostname) {
			fmt.Printf("Error: wrong hostname: [%s]\n", vm.Host_hostname)
			JSONError(w, "host_hostname should be valid form. valid form", http.StatusInternalServerError)
			return
		} else {
			fmt.Printf("Found host_hostname: [%s]\n", vm.Host_hostname)
		}
	}

	if len(vm.Extras) > 1 {
		if !regexpExtras.MatchString(vm.Extras) {
			fmt.Printf("Error: wrong extras: [%s]\n", vm.Extras)
			JSONError(w, "extras should be valid form. valid form", http.StatusInternalServerError)
			return
		} else {
			fmt.Printf("Found extras: [%s]\n", vm.Extras)
		}
	}

	if len(vm.Recomendation) > 1 {
		if !regexpHostName.MatchString(vm.Recomendation) {
			fmt.Printf("Error: wrong hostname recomendation: [%s]\n", vm.Recomendation)
			JSONError(w, "recomendation should be valid form. valid form", http.StatusInternalServerError)
			return
		} else {
			fmt.Printf("Found vm recomendation: [%s]\n", vm.Recomendation)
			suggest = vm.Recomendation
		}
	} else {
		suggest = ""
	}

	// not for jail yet
	if strings.Compare(vm.Type, "bhyve") == 0 {
		// master value validation
		cpus, err := strconv.Atoi(vm.Cpus)
		fmt.Printf("C: [%s] [%d]\n", vm.Cpus, vm.Cpus)
		if err != nil {
			JSONError(w, "cpus not a number", http.StatusInternalServerError)
			return
		}
		if cpus <= 0 || cpus > 10 {
			JSONError(w, "cpus valid range: 1-16", http.StatusInternalServerError)
			return
		}
	} else {
		vm.Cpus = "0"
	}

	if strings.Compare(vm.Type, "bhyve") == 0 {
		if !regexpSize.MatchString(vm.Ram) {
			JSONError(w, "The ram should be valid form, 512m, 1g", http.StatusInternalServerError)
			return
		}
	} else {
		vm.Ram = "0"
	}

	if !regexpSize.MatchString(vm.Imgsize) {
		JSONError(w, "The imgsize should be valid form, 2g, 30g", http.StatusInternalServerError)
		return
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
	//str.WriteString("}}");

	// todo: filter for insecured param=val
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
		buf := fmt.Sprintf(",\"%s\": \"%s\"", jconf_param, tmpval)
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
	fmt.Printf("C: [%s]\n", str.String())
	response := fmt.Sprintf("{ \"Message\": [\"curl -H cid:%x %s/api/v1/cluster\", \"curl -H cid:%x %s/api/v1/status/%s\", \"curl -H cid:%x %s/api/v1/start/%s\", \"curl -H cid:%x %s/api/v1/stop/%s\", \"curl -H cid:%x %s/api/v1/destroy/%s\"] }", cid, server_url, cid, server_url, InstanceId, cid, server_url, InstanceId, cid, server_url, InstanceId, cid, server_url, InstanceId)

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	getNodeRecomendation(recomendation.String(), suggest)
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

func (feeds *MyFeeds) HandleClusterDestroy(w http.ResponseWriter, r *http.Request) {
	var InstanceId string
	params := mux.Vars(r)

	InstanceId = params["InstanceId"]

	if !validateInstanceId(InstanceId) {
		JSONError(w, "The InstanceId should be valid form: ^[a-z_]([a-z0-9_])*$ (maxlen: 40)", http.StatusNotFound)
		return
	}

	Cid := r.Header.Get("cid")
	if !validateCid(Cid) {
		JSONError(w, "The cid should be valid form: ^[a-f0-9]{32}$", http.StatusNotFound)
		return
	}

	if !isCidAllowed(feeds, Cid) {
		fmt.Printf("CID not in ACL: %s\n", Cid)
		JSONError(w, "not allowed", http.StatusInternalServerError)
		return
	}

	HomePath := fmt.Sprintf("%s/%s/vms", *dbDir, Cid)
	if _, err := os.Stat(HomePath); os.IsNotExist(err) {
		fmt.Println("path not found:", HomePath)
		JSONError(w, "not found", http.StatusNotFound)
		return
	}

	mapfile := fmt.Sprintf("%s/var/db/api/map/%s-%s", workdir, Cid, InstanceId)

	if !fileExists(config.Recomendation) {
		fmt.Printf("no such map file %s/var/db/api/map/%s-%s\n", workdir, Cid, InstanceId)
		JSONError(w, "not found", http.StatusNotFound)
		return
	}

	b, err := ioutil.ReadFile(mapfile) // just pass the file name
	if err != nil {
		fmt.Printf("unable to read jname from %s/var/db/api/map/%s-%s\n", workdir, Cid, InstanceId)
		JSONError(w, "not found", http.StatusNotFound)
		return
	}

	fmt.Printf("Destroy %s via %s/var/db/api/map/%x-%s\n", string(b), workdir, Cid, InstanceId)

	// of course we can use marshal here instead of string concatenation,
	// but now this is too simple case/data without any processing
	var str strings.Builder

	// destroy via
	runscript = *destroyScript
	str.WriteString("{\"Command\":\"")
	str.WriteString(runscript)
	str.WriteString("\",\"CommandArgs\":{\"mode\":\"destroy\",\"jname\":\"")
	str.WriteString(string(b))
	str.WriteString("\"")
	str.WriteString("}}")

	//get guest nodes & tubes
	SqliteDBPath := fmt.Sprintf("%s/%s/%s.node", *dbDir, Cid, string(b))
	if fileExists(SqliteDBPath) {
		b, err := ioutil.ReadFile(SqliteDBPath) // just pass the file name
		if err != nil {
			fmt.Printf("unable to read node map: %s\n", SqliteDBPath)
			JSONError(w, "unable to read node map", http.StatusNotFound)
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
		JSONError(w, "unable to read node map", http.StatusNotFound)
		return
	}

	fmt.Printf("C: [%s]\n", str.String())
	go realInstanceCreate(str.String())

	e := os.Remove(mapfile)
	if e != nil {
		log.Fatal(e)
	}

	// remove from FS
	VmPath := fmt.Sprintf("%s/%s/vm-%s", *dbDir, Cid, InstanceId)
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

			VmPath = fmt.Sprintf("%s/%s/%s-bhyve.ssh", *dbDir, Cid, string(b))
			fmt.Printf("   REMOVE: %s\n", VmPath)
			e = os.Remove(VmPath)

			VmPath = fmt.Sprintf("%s/%s/vms/%s", *dbDir, Cid, string(b))
			fmt.Printf("   REMOVE: %s\n", VmPath)
			e = os.Remove(VmPath)
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
		JSONError(w, "The InstanceId should be valid form: ^[a-z_]([a-z0-9_])*$ (maxlen: 40)", http.StatusNotFound)
		return
	}

	Cid := r.Header.Get("cid")
	if !validateCid(Cid) {
		JSONError(w, "The cid should be valid form: ^[a-f0-9]{32}$", http.StatusNotFound)
		return
	}

	if !isCidAllowed(feeds, Cid) {
		fmt.Printf("CID not in ACL: %s\n", Cid)
		JSONError(w, "not allowed", http.StatusInternalServerError)
		return
	}

	HomePath := fmt.Sprintf("%s/%s/vms", *dbDir, Cid)
	if _, err := os.Stat(HomePath); os.IsNotExist(err) {
		return
	}

	mapfile := fmt.Sprintf("%s/var/db/api/map/%s-%s", workdir, Cid, InstanceId)

	if !fileExists(config.Recomendation) {
		fmt.Printf("no such map file %s/var/db/api/map/%s-%s\n", workdir, Cid, InstanceId)
		JSONError(w, "not found", http.StatusNotFound)
		return
	}

	b, err := ioutil.ReadFile(mapfile) // just pass the file name
	if err != nil {
		fmt.Printf("unable to read jname from %s/var/db/api/map/%s-%s\n", workdir, Cid, InstanceId)
		JSONError(w, "not found", http.StatusNotFound)
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
		JSONError(w, "nodes not found", http.StatusNotFound)
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
		JSONError(w, "The InstanceId should be valid form: ^[a-z_]([a-z0-9_])*$ (maxlen: 40)", http.StatusNotFound)
		return
	}

	Cid := r.Header.Get("cid")
	if !validateCid(Cid) {
		JSONError(w, "The cid should be valid form: ^[a-f0-9]{32}$", http.StatusNotFound)
		return
	}

	if !isCidAllowed(feeds, Cid) {
		fmt.Printf("CID not in ACL: %s\n", Cid)
		JSONError(w, "not allowed", http.StatusInternalServerError)
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
		JSONError(w, "not found", http.StatusNotFound)
		return
	}

	b, err := ioutil.ReadFile(mapfile) // just pass the file name
	if err != nil {
		fmt.Printf("unable to read jname from %s/var/db/api/map/%s-%s\n", workdir, Cid, InstanceId)
		JSONError(w, "not found", http.StatusNotFound)
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
		JSONError(w, "nodes not found", http.StatusNotFound)
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
