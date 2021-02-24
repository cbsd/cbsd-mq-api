// CBSD Project 2013-2021
package main

import (
	"encoding/json"
	"github.com/gorilla/mux"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"fmt"
	"reflect"
	"flag"
	"io/ioutil"
	"crypto/md5"
	"os/exec"
	"golang.org/x/crypto/ssh"
)

var lock = sync.RWMutex{}
var config	Config
var runscript	string
var workdir	string
var server_url	string

type Response struct {
	Message    string
}

// The cluster Type. Name of elements must match with jconf params
type Vm struct {
	Type		string	`json:type,omitempty"`
	Jname		string	`json:jname,omitempty"`
	Img		string	`json:img,omitempty"`
	Ram		string	`json:ram,omitempty"`
	Cpus		string	`"cpus,omitempty"`
	Imgsize		string	`"imgsize,omitempty"`
	Pubkey		string	`"pubkey,omitempty"`
}

// Todo: validate mod?
//  e.g for simple check:
//  bhyve_name  string `json:"name" validate:"required,min=2,max=100"`
var (
	body		= flag.String("body", "", "Body of message")
	cbsdEnv		= flag.String("cbsdenv", "/usr/jails", "CBSD workdir environment")
	configFile	= flag.String("config", "/usr/local/etc/cbsd-mq-api.json", "Path to config.json")
	listen *string	= flag.String("listen", "0.0.0.0:65531", "Listen host:port")
	runScriptJail	= flag.String("runscript_jail", "jail-api", "CBSD target run script")
	runScriptBhyve	= flag.String("runscript_bhyve", "bhyve-api", "CBSD target run script")
	destroyScript	= flag.String("destroy_script", "control-api", "CBSD target run script")
	startScript	= flag.String("start_script", "control-api", "CBSD target run script")
	stopScript	= flag.String("stop_script", "control-api", "CBSD target run script")
	serverUrl	= flag.String("server_url", "http://127.0.0.1:65532", "Server URL for external requests") 
	dbDir		= flag.String("dbdir", "/var/db/cbsd-api", "db root dir")
)

func fileExists(filename string) bool {
//	info, err := os.Stat(filename)
	_, err := os.Stat(filename)
	if os.IsNotExist(err) {
		return false
	}
//	return !info.IsDir()
	return true
}

// main function to boot up everything
func main() {

	flag.Parse()
	var err error

	config, err = LoadConfiguration(*configFile)

	workdir=config.CbsdEnv
	server_url=config.ServerUrl

	if err != nil {
		fmt.Println("config load error")
		os.Exit(1)
	}

	if !fileExists(config.Recomendation) {
		fmt.Printf("no such Recomendation script, please check config/path: %s\n",config.Recomendation)
		os.Exit(1)
	}
	if !fileExists(config.Freejname) {
		fmt.Printf("no such Freejname script, please check config/path: %s\n",config.Freejname)
		os.Exit(1)
	}

	if !fileExists(*dbDir) {
		fmt.Printf("db dir created: %s\n",*dbDir)
		os.MkdirAll(*dbDir, 0770)
	}

	router := mux.NewRouter()
	router.HandleFunc("/api/v1/create/{instanceid}", HandleClusterCreate).Methods("POST")
	router.HandleFunc("/api/v1/status/{instanceid}", HandleClusterStatus).Methods("GET")
	router.HandleFunc("/api/v1/start/{instanceid}", HandleClusterStart).Methods("GET")
	router.HandleFunc("/api/v1/stop/{instanceid}", HandleClusterStop).Methods("GET")
	router.HandleFunc("/api/v1/cluster", HandleClusterCluster).Methods("GET")
	router.HandleFunc("/api/v1/destroy/{instanceid}", HandleClusterDestroy).Methods("GET")
	fmt.Println("Listen",*listen)
	fmt.Println("Server URL",server_url)
	log.Fatal(http.ListenAndServe(*listen, router))
}

func HandleClusterStatus(w http.ResponseWriter, r *http.Request) {
	var instanceid string
	params := mux.Vars(r)
	instanceid = params["instanceid"]
	var regexpInstanceId = regexp.MustCompile(`^[aA-zZ_]([aA-zZ0-9_])*$`)

	Cid := r.Header.Get("cid")
	HomePath := fmt.Sprintf("%s/%s/vms", *dbDir, Cid)
	//fmt.Println("CID IS: [ %s ]", cid)
	if _, err := os.Stat(HomePath); os.IsNotExist(err) {
		return
	}

	// check the name field is between 3 to 40 chars
	if len(instanceid) < 3 || len(instanceid) > 40 {
		http.Error(w, "The instance name must be between 3-40", 400)
		return
	}
	if !regexpInstanceId.MatchString(instanceid) {
		http.Error(w, "The instance name should be valid form, ^[aA-zZ_]([aA-zZ0-9_])*$", 400)
		return
	}

	mapfile := fmt.Sprintf("%s/var/db/api/map/%s-%s", workdir, Cid,instanceid)

	if !fileExists(config.Recomendation) {
		fmt.Printf("no such map file %s/var/db/api/map/map/%s-%s\n",workdir, Cid, instanceid)
		response := Response{"no found"}
		js, err := json.Marshal(response)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Error(w, string(js), http.StatusNotFound)
		return
	}

	b, err := ioutil.ReadFile(mapfile) // just pass the file name
	if err != nil {
		fmt.Printf("unable to read jname from %s/var/db/api/map/%s-%s\n",workdir, Cid, instanceid)
		response := Response{"no found"}
		js, err := json.Marshal(response)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Error(w, string(js), http.StatusNotFound)
		return
	}

	SqliteDBPath := fmt.Sprintf("%s/%s/%s-bhyve.ssh", *dbDir, Cid,string(b))
	if fileExists(SqliteDBPath) {
		b, err := ioutil.ReadFile(SqliteDBPath) // just pass the file name
		if err != nil {
			http.Error(w, "{}", 400)
			return
		} else {
			// when json:
			//response := Response{string(b)}
			//js, err := json.Marshal(response)
			//if err != nil {
			//	http.Error(w, err.Error(), http.StatusInternalServerError)
			//	return
			//}
			// when human:
			js := string(b)
			http.Error(w, string(js), 400)
			return
		}
	} else {
		http.Error(w, "{}", 400)
	}
}

func HandleClusterCluster(w http.ResponseWriter, r *http.Request) {
	Cid := r.Header.Get("cid")
	HomePath := fmt.Sprintf("%s/%s/vms", *dbDir, Cid)
	//fmt.Println("CID IS: [ %s ]", cid)
	if _, err := os.Stat(HomePath); os.IsNotExist(err) {
		return
	}

	SqliteDBPath := fmt.Sprintf("%s/%s/vm.list", *dbDir, Cid)
	if fileExists(SqliteDBPath) {
		b, err := ioutil.ReadFile(SqliteDBPath) // just pass the file name
		if err != nil {
			http.Error(w, "{}", 400)
			return
		} else {
			http.Error(w, string(b), 200)
			return
		}
	} else {
		http.Error(w, "{}", 400)
	}
}


func realInstanceCreate(body string) {

	a := &body

	stdout, err := beanstalkSend(config.BeanstalkConfig, *a)
	fmt.Printf("%s\n",stdout);

	if err != nil {
		return
	}
}

func getNodeRecomendation(body string) {
	cmdStr := fmt.Sprintf("%s %s", config.Recomendation,body)
	//cmdStr := fmt.Sprintf("/root/api/get_recomendation.sh %s", body)
	cmdArgs := strings.Fields(cmdStr)
	cmd := exec.Command(cmdArgs[0], cmdArgs[1:len(cmdArgs)]...)
	out, err := cmd.CombinedOutput()
	if err != nil {
			fmt.Println("/root/api/get_recomendation.sh failed")
	}
	result := (string(out))
	fmt.Printf("Host Recomendation: [%s]\n",result)

	result = strings.Replace(result, ".", "_", -1)
	result = strings.Replace(result, "-", "_", -1)

	tube := fmt.Sprintf("cbsd_%s",result)
	reply := fmt.Sprintf("cbsd_%s_result_id",result)

	fmt.Printf("Tube selected: [%s]\n",tube)
	fmt.Printf("ReplyTube selected: [%s]\n",reply)

	config.BeanstalkConfig.Tube=tube
	config.BeanstalkConfig.ReplyTubePrefix=reply
}


func getJname() string {
	cmdStr := fmt.Sprintf("%s", config.Freejname)
	cmdArgs := strings.Fields(cmdStr)
	cmd := exec.Command(cmdArgs[0], cmdArgs[1:len(cmdArgs)]...)
	out, err := cmd.CombinedOutput()
	if err != nil {
			fmt.Println("/root/api/get_recomendation.sg failed")
	}
	result := (string(out))
	fmt.Printf("Freejname Recomendation: [%s]\n",result)
	return result
}

func HandleClusterCreate(w http.ResponseWriter, r *http.Request) {
	var instanceid string
	params := mux.Vars(r)
	instanceid = params["instanceid"]
	var regexpInstanceId = regexp.MustCompile(`^[aA-zZ_]([aA-zZ0-9_])*$`)
	var regexpSize = regexp.MustCompile(`^[1-9](([0-9]+)?)([m|g|t])$`)
	var regexpPubkey = regexp.MustCompile("^(ssh-rsa|ssh-dss|ssh-ed25519|ecdsa-[^ ]+) ([^ ]+) ?(.*)")

	w.Header().Set("Content-Type", "application/json")

	// check the name field is between 3 to 40 chars
	if len(instanceid) < 2 || len(instanceid) > 40 {
		response := Response{"The instance name must be between 2-40"}
		js, err := json.Marshal(response)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Error(w, string(js), 400)
		return
	}
	if !regexpInstanceId.MatchString(instanceid) {
		response := Response{"The instance name should be valid form, ^[aA-zZ_]([aA-zZ0-9_])*$"}
		js, err := json.Marshal(response)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Error(w, string(js), 400)
		return
	}


	if r.Body == nil {
		response := Response{"please send a request body"}
		js, err := json.Marshal(response)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Error(w, string(js), 400)
		return
	}

	fmt.Println("create wakeup")

	var vm Vm
	_ = json.NewDecoder(r.Body).Decode(&vm)

	switch vm.Type {
		case "jail":
			fmt.Println(vm.Type, "type selected")
			runscript = *runScriptJail
		case "bhyve":
			fmt.Println(vm.Type, "type selected")
			runscript = *runScriptBhyve
		default:
			fmt.Println("Unknown resource type:", vm.Type, "valid: 'bhyve', 'jail'")
			response := Response{"unknown resource type"}
			js, err := json.Marshal(response)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			http.Error(w, string(js), 400)
			return
	}

	if ( len(vm.Pubkey)<30 ) {
		fmt.Printf("Error: Pubkey too small: []\n",vm.Pubkey)
		response := Response{"Pubkey too small"}
		js, err := json.Marshal(response)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Error(w, string(js), 400)
		return
	}

	if ( len(vm.Pubkey)>500 ) {
		response := Response{"Pubkey too long"}
		js, err := json.Marshal(response)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Error(w, string(js), 400)
		return
	}

	if !regexpPubkey.MatchString(vm.Pubkey) {
		response := Response{"pubkey should be valid form. valid key: ssh-rsa,ssh-ed25519,ecdsa-*,ssh-dsa XXXXX comment"}
		js, err := json.Marshal(response)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Error(w, string(js), 400)
		return
	}

	parsedKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(vm.Pubkey))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	fmt.Printf("pubKey: [%x]\n",parsedKey)

	uid := []byte(vm.Pubkey)

	// master value validation
	cpus, err := strconv.Atoi(vm.Cpus)
	fmt.Printf("C: [%s] [%d]\n",vm.Cpus, vm.Cpus)
	if err != nil {
		response := Response{"cpus not a number"}
		js, err := json.Marshal(response)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Error(w, string(js), 400)
		return
	}
	if cpus <= 0 || cpus > 10 {
		response := Response{"Cpus valid range: 1-16"}
		js, err := json.Marshal(response)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Error(w, string(js), 400)
		return
	}

	if !regexpSize.MatchString(vm.Ram) {
		response := Response{"The ram should be valid form, 512m, 1g"}
		js, err := json.Marshal(response)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Error(w, string(js), 400)
		return
	}
	if !regexpSize.MatchString(vm.Imgsize) {
		response := Response{"The imgsize should be valid form, 2g, 30g"}
		js, err := json.Marshal(response)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Error(w, string(js), 400)
		return
	}

	//existance?
	// check for existance
	cid := md5.Sum(uid)
	VmPathDir := fmt.Sprintf("%s/%x", *dbDir, cid)

	if !fileExists(VmPathDir) {
		os.Mkdir(VmPathDir, 0775)
	}

	VmPath := fmt.Sprintf("%s/%x/vm-%s", *dbDir, cid,instanceid)
	fmt.Println(*dbDir, "instance exist")

	if fileExists(VmPath) {
		response := Response{"vm already exist"}
		js, err := json.Marshal(response)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Error(w, string(js), 400)
		return
	}
	// create empty file
	f, err := os.Create(VmPath)

	if err != nil {
		log.Fatal(err)
	}


	Jname := getJname()
	fmt.Printf("GET NEXT FREE JNAME: [%s]\n",Jname)

	_, err2 := f.WriteString(Jname)

	if err2 != nil {
		log.Fatal(err2)
	}

	f.Close()

	vm.Jname = instanceid
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

	for i := 0; i < val.NumField(); i++ {
		valueField := val.Field(i)

		typeField := val.Type().Field(i)
		tag := typeField.Tag

		tmpval := fmt.Sprintf("%s",valueField.Interface())

		if len(tmpval) == 0 {
			continue
		}

		fmt.Printf("[%s]",valueField);

		jconf_param = strings.ToLower(typeField.Name)
		if strings.Compare(jconf_param,"jname") == 0 {
			continue
		}
		fmt.Printf("jconf: %s,\tField Name: %s,\t Field Value: %v,\t Tag Value: %s\n", jconf_param, typeField.Name, valueField.Interface(), tag.Get("tag_name"))
		buf := fmt.Sprintf(",\"%s\": \"%s\"", jconf_param, tmpval)
		buf2 := fmt.Sprintf("%s ", tmpval)
		str.WriteString(buf)
		recomendation.WriteString(buf2)
	}

	str.WriteString(",\"host_hostname\": \"")
	str.WriteString(instanceid)
	str.WriteString("\"}}");
	fmt.Printf("C: [%s]\n",str.String())
	response := fmt.Sprintf("API:\ncurl -H \"cid:%x\" %s/api/v1/cluster\ncurl -H \"cid:%x\" %s/api/v1/status/%s\ncurl -H \"cid:%x\" %s/api/v1/start/%s\ncurl -H \"cid:%x\" %s/api/v1/stop/%s\ncurl -H \"cid:%x\" %s/api/v1/destroy/%s\n", cid, server_url, cid, server_url, instanceid, cid, server_url, instanceid, cid, server_url, instanceid, cid, server_url, instanceid)
//	md5uid := cid
//	response := string(md5uid[:])

//	js, err := json.Marshal(response)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	getNodeRecomendation(recomendation.String())
	go realInstanceCreate(str.String())

	mapfile := fmt.Sprintf("%s/var/db/api/map/%x-%s", workdir, cid,instanceid)
	m, err := os.Create(mapfile)

	if err != nil {
		log.Fatal(err)
	}

	_, err3 := m.WriteString(Jname)

	if err3 != nil {
		log.Fatal(err3)
	}

	m.Close()

	http.Error(w, response, 200)

	return
}


func HandleClusterDestroy(w http.ResponseWriter, r *http.Request) {
	var instanceid string
	params := mux.Vars(r)
	instanceid = params["instanceid"]
	var regexpInstanceId = regexp.MustCompile(`^[aA-zZ_]([aA-zZ0-9_])*$`)

	Cid := r.Header.Get("cid")
	HomePath := fmt.Sprintf("%s/%s/vms", *dbDir, Cid)
	if _, err := os.Stat(HomePath); os.IsNotExist(err) {
		fmt.Println("path not found:", HomePath)
		response := Response{"env no found"}
		js, err := json.Marshal(response)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Error(w, string(js), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	// check the name field is between 3 to 40 chars
	if len(instanceid) < 3 || len(instanceid) > 40 {
		http.Error(w, "The instance name must be between 3-40", 400)
		return
	}
	if !regexpInstanceId.MatchString(instanceid) {
		http.Error(w, "The instance name should be valid form, ^[aA-zZ_]([aA-zZ0-9_])*$", 400)
		return
	}

	mapfile := fmt.Sprintf("%s/var/db/api/map/%s-%s", workdir, Cid,instanceid)

	if !fileExists(config.Recomendation) {
		fmt.Printf("no such map file %s/var/db/api/map/%s-%s\n",workdir, Cid, instanceid)
		response := Response{"no found"}
		js, err := json.Marshal(response)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Error(w, string(js), http.StatusNotFound)
		return
	}

	b, err := ioutil.ReadFile(mapfile) // just pass the file name
	if err != nil {
		fmt.Printf("unable to read jname from %s/var/db/api/map/%s-%s\n",workdir, Cid, instanceid)
		response := Response{"no found"}
		js, err := json.Marshal(response)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Error(w, string(js), http.StatusNotFound)
		return
	}

	fmt.Printf("Destroy %s via %s/var/db/api/map/%x-%s\n",string(b), workdir, Cid, instanceid)

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
	str.WriteString("}}");

	//get guest nodes & tubes
	SqliteDBPath := fmt.Sprintf("%s/%s/%s.node", *dbDir, Cid,string(b))
	if fileExists(SqliteDBPath) {
		b, err := ioutil.ReadFile(SqliteDBPath) // just pass the file name
		if err != nil {
			response := Response{"unabe to read node map"}
			js, err := json.Marshal(response)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			http.Error(w, string(js), http.StatusNotFound)
			return
		} else {
			result := strings.Replace(string(b), ".", "_", -1)
			result = strings.Replace(result, "-", "_", -1)
			result = strings.TrimSuffix(result, "\n")
		//	result = strings.Replace(result, "\r\n", "", -1)

			tube := fmt.Sprintf("cbsd_%s",result)
			reply := fmt.Sprintf("cbsd_%s_result_id",result)

			fmt.Printf("Tube selected: [%s]\n",tube)
			fmt.Printf("ReplyTube selected: [%s]\n",reply)

			// result: srv-03.olevole.ru
			config.BeanstalkConfig.Tube=tube
			config.BeanstalkConfig.ReplyTubePrefix=reply
		}
	} else {
		response := Response{"unabe to open node map"}
		js, err := json.Marshal(response)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Error(w, string(js), http.StatusNotFound)
		return
	}

	fmt.Printf("C: [%s]\n",str.String())
	go realInstanceCreate(str.String())

	e := os.Remove(mapfile)
	if e != nil { 
		log.Fatal(e) 
	}

	// remove from FS
	VmPath := fmt.Sprintf("%s/%s/vm-%s", *dbDir,Cid,instanceid)
	if fileExists(VmPath) {
		b, err := ioutil.ReadFile(VmPath) // just pass the file name
		if err != nil {
			fmt.Printf("Error read UID from  [%s]\n",string(b))
		} else {

			fmt.Printf("   REMOVE: %s\n",VmPath)
			e = os.Remove(VmPath)

			VmPath = fmt.Sprintf("%s/%s/%s.node", *dbDir,Cid,string(b))
			fmt.Printf("   REMOVE: %s\n",VmPath)
			e = os.Remove(VmPath)

			VmPath = fmt.Sprintf("%s/%s/%s-bhyve.ssh", *dbDir,Cid,string(b))
			fmt.Printf("   REMOVE: %s\n",VmPath)
			e = os.Remove(VmPath)

			VmPath = fmt.Sprintf("%s/%s/vms/%s", *dbDir,Cid,string(b))
			fmt.Printf("   REMOVE: %s\n",VmPath)
			e = os.Remove(VmPath)
		}
	}

	response := Response{"destroy"}
	js, err := json.Marshal(response)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Error(w, string(js), 200)
	return
}

func HandleClusterStop(w http.ResponseWriter, r *http.Request) {
	var instanceid string
	params := mux.Vars(r)
	instanceid = params["instanceid"]
	var regexpInstanceId = regexp.MustCompile(`^[aA-zZ_]([aA-zZ0-9_])*$`)

	Cid := r.Header.Get("cid")
	HomePath := fmt.Sprintf("%s/%s/vms", *dbDir, Cid)
	if _, err := os.Stat(HomePath); os.IsNotExist(err) {
		return
	}

	w.Header().Set("Content-Type", "application/json")

	// check the name field is between 3 to 40 chars
	if len(instanceid) < 3 || len(instanceid) > 40 {
		http.Error(w, "The instance name must be between 3-40", 400)
		return
	}
	if !regexpInstanceId.MatchString(instanceid) {
		http.Error(w, "The instance name should be valid form, ^[aA-zZ_]([aA-zZ0-9_])*$", 400)
		return
	}

	mapfile := fmt.Sprintf("%s/var/db/api/map/%s-%s", workdir, Cid,instanceid)

	if !fileExists(config.Recomendation) {
		fmt.Printf("no such map file %s/var/db/api/map/%s-%s\n",workdir, Cid, instanceid)
		response := Response{"no found"}
		js, err := json.Marshal(response)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Error(w, string(js), http.StatusNotFound)
		return
	}

	b, err := ioutil.ReadFile(mapfile) // just pass the file name
	if err != nil {
		fmt.Printf("unable to read jname from %s/var/db/api/map/%s-%s\n",workdir, Cid, instanceid)
		response := Response{"no found"}
		js, err := json.Marshal(response)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Error(w, string(js), http.StatusNotFound)
		return
	}

	fmt.Printf("stop %s via %s/var/db/api/map/%s-%s\n",string(b), workdir, Cid, instanceid)

	// of course we can use marshal here instead of string concatenation,
	// but now this is too simple case/data without any processing
	var str strings.Builder

	runscript = *stopScript
	str.WriteString("{\"Command\":\"")
	str.WriteString(runscript)
	str.WriteString("\",\"CommandArgs\":{\"mode\":\"stop\",\"jname\":\"")
	str.WriteString(string(b))
	str.WriteString("\"")
	str.WriteString("}}");

	//get guest nodes & tubes
	SqliteDBPath := fmt.Sprintf("%s/%s/%s.node", *dbDir, Cid,string(b))
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

			tube := fmt.Sprintf("cbsd_%s",result)
			reply := fmt.Sprintf("cbsd_%s_result_id",result)

			fmt.Printf("Tube selected: [%s]\n",tube)
			fmt.Printf("ReplyTube selected: [%s]\n",reply)

			// result: srv-03.olevole.ru
			config.BeanstalkConfig.Tube=tube
			config.BeanstalkConfig.ReplyTubePrefix=reply
		}
	} else {
		response := Response{"nodes node found"}
		js, err := json.Marshal(response)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Error(w, string(js), 200)
		return
	}

	fmt.Printf("C: [%s]\n",str.String())
	go realInstanceCreate(str.String())

	// remove from FS
	VmPath := fmt.Sprintf("%s/%s/vm-%s", *dbDir, Cid,instanceid)
	if fileExists(VmPath) {
		b, err := ioutil.ReadFile(VmPath) // just pass the file name
		if err != nil {
			fmt.Printf("Error read UID from  [%s]\n",string(b))
		}
	}

	response := Response{"stopped"}
	js, err := json.Marshal(response)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Error(w, string(js), 200)
	return
}


func HandleClusterStart(w http.ResponseWriter, r *http.Request) {
	var instanceid string
	params := mux.Vars(r)
	instanceid = params["instanceid"]
	var regexpInstanceId = regexp.MustCompile(`^[aA-zZ_]([aA-zZ0-9_])*$`)

	Cid := r.Header.Get("cid")
	HomePath := fmt.Sprintf("%s/%s/vms", *dbDir,Cid)
	if _, err := os.Stat(HomePath); os.IsNotExist(err) {
		return
	}

	w.Header().Set("Content-Type", "application/json")

	// check the name field is between 3 to 40 chars
	if len(instanceid) < 3 || len(instanceid) > 40 {
		http.Error(w, "The instance name must be between 3-40", 400)
		return
	}
	if !regexpInstanceId.MatchString(instanceid) {
		http.Error(w, "The instance name should be valid form, ^[aA-zZ_]([aA-zZ0-9_])*$", 400)
		return
	}

	mapfile := fmt.Sprintf("%s/var/db/api/map/%s-%s", workdir, Cid,instanceid)

	if !fileExists(config.Recomendation) {
		fmt.Printf("no such map file %s/var/db/api/map/%s-%s\n",workdir, Cid, instanceid)
		response := Response{"no found"}
		js, err := json.Marshal(response)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Error(w, string(js), http.StatusNotFound)
		return
	}

	b, err := ioutil.ReadFile(mapfile) // just pass the file name
	if err != nil {
		fmt.Printf("unable to read jname from %s/var/db/api/map/%s-%s\n",workdir, Cid, instanceid)
		response := Response{"no found"}
		js, err := json.Marshal(response)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Error(w, string(js), http.StatusNotFound)
		return
	}

	fmt.Printf("start %s via %s/var/db/api/map/%s-%s\n",string(b), workdir, Cid, instanceid)

	// of course we can use marshal here instead of string concatenation,
	// but now this is too simple case/data without any processing
	var str strings.Builder

	runscript = *startScript
	str.WriteString("{\"Command\":\"")
	str.WriteString(runscript)
	str.WriteString("\",\"CommandArgs\":{\"mode\":\"start\",\"jname\":\"")
	str.WriteString(string(b))
	str.WriteString("\"")
	str.WriteString("}}");

	//get guest nodes & tubes
	SqliteDBPath := fmt.Sprintf("%s/%s/%s.node", *dbDir,Cid,string(b))
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

			tube := fmt.Sprintf("cbsd_%s",result)
			reply := fmt.Sprintf("cbsd_%s_result_id",result)

			fmt.Printf("Tube selected: [%s]\n",tube)
			fmt.Printf("ReplyTube selected: [%s]\n",reply)

			// result: srv-03.olevole.ru
			config.BeanstalkConfig.Tube=tube
			config.BeanstalkConfig.ReplyTubePrefix=reply
		}
	} else {
		response := Response{"nodes node found"}
		js, err := json.Marshal(response)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Error(w, string(js), 200)
		return
	}

	fmt.Printf("C: [%s]\n",str.String())
	go realInstanceCreate(str.String())

	// remove from FS
	VmPath := fmt.Sprintf("%s/%s/vm-%s", *dbDir,Cid,instanceid)
	if fileExists(VmPath) {
		b, err := ioutil.ReadFile(VmPath) // just pass the file name
		if err != nil {
			fmt.Printf("Error read UID from  [%s]\n",string(b))
		}
	}

	response := Response{"started"}
	js, err := json.Marshal(response)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Error(w, string(js), 200)
	return
}

