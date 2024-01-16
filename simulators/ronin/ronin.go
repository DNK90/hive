package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"time"

	"github.com/ethereum/hive/hivesim"
	"github.com/ethereum/hive/internal/simapi"
	"gopkg.in/inconshreveable/log15.v2"
)

var (
	validators = []map[string]string{
		{
			"privateKey": "e24d25ccef5e9d01071c1216c70393f830007090a9c40779cb23cad908d95404",
			"address":    "af96c12e5e2d69c5f721fe1430b6d22a07748be6",
		},
		{
			"privateKey": "2d4b1e6ed6e469037f5c41abfdb07e275c1cc4c0797a169a2a46dacc8750a2b4",
			"address":    "da0a817cd6ac3521ee669637fffb7c6293014840",
		},
	}
	params = hivesim.Params{
		"HIVE_FORK_CONSORTIUMV2":      "1000000000",
		"HIVE_RONIN_VALIDATOR_SET":    "0x0000000000000000000000000000000000000123",
		"HIVE_RONIN_SLASH_INDICATOR":  "0x0000000000000000000000000000000000000456",
		"HIVE_RONIN_STAKING_CONTRACT": "0x0000000000000000000000000000000000000789",
		"HIVE_CONSORTIUM_PERIOD":      "3",
		"HIVE_CONSORTIUM_EPOCH":       "30",
		"HIVE_RONIN_PRIVATEKEY":       "",
		"HIVE_MINER":                  "",
		"HIVE_CHAIN_ID":               "1334",
		"HIVE_FORK_HOMESTEAD":         "0",
		"HIVE_FORK_TANGERINE":         "0",
		"HIVE_FORK_SPURIOUS":          "0",
		"HIVE_FORK_BYZANTIUM":         "0",
		"HIVE_FORK_CONSTANTINOPLE":    "0",
		"HIVE_FORK_PETERSBURG":        "0",
		"HIVE_VALIDATOR_1_SLOT_VALUE": "0x000000000000000000000000af96c12e5e2d69c5f721fe1430b6d22a07748be6",
		"HIVE_VALIDATOR_2_SLOT_VALUE": "0x000000000000000000000000da0a817cd6ac3521ee669637fffb7c6293014840",
		"HIVE_MAIN_ACCOUNT":           "0xaf96c12e5e2d69c5f721fe1430b6d22a07748be6",
	}
)

func main() {
	suite := hivesim.Suite{
		Name:        "ronin",
		Description: "This test suite tests ronin mining support.",
	}
	suite.Add(hivesim.TestSpec{
		Name: "test file loader",
		Description: "This is a meta-test. It loads the blockchain test files and " +
			"launches the actual client tests. Any errors in test files will be reported " +
			"through this test.",
		Run:       loaderTest,
		AlwaysRun: true,
	})
	hivesim.MustRunSuite(hivesim.New(), suite)
}

type block struct {
	Number     string `json:"number"`
	Hash       string `json:"hash"`
	ParentHash string `json:"parentHash"`
}

func loaderTest(t *hivesim.T) {
	log15.Info("loading test")
	// list all files or directory in current working dir
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}

	for _, e := range entries {
		t.Log(e.Name())
	}

	mountSource, ok := os.LookupEnv("RONIN_CHAINDATA")
	if !ok {
		t.Fatal("mount source is undefined")
	}

	params1 := params.Copy()
	params1["HIVE_RONIN_PRIVATEKEY"] = validators[0]["privateKey"]
	params1["HIVE_MINER"] = validators[0]["address"]

	params2 := params.Copy()
	params2["HIVE_RONIN_PRIVATEKEY"] = validators[1]["privateKey"]
	params2["HIVE_MINER"] = validators[1]["address"]

	// start 2 client with 2 above params
	client1 := t.StartClient("ronin", params1, hivesim.WithMounts(simapi.Mount{
		RW:          true,
		Source:      fmt.Sprintf("%s/node1", mountSource),
		Destination: "/root/.ethereum/ronin",
	}))

	enode, err := client1.EnodeURLNetwork("bridge")
	if err != nil {
		t.Error(err)
	}
	params2["HIVE_BOOTNODE"] = enode

	// start client2
	t.StartClient("ronin", params2, hivesim.WithMounts(simapi.Mount{
		RW:          true,
		Source:      fmt.Sprintf("%s/node2", mountSource),
		Destination: "/root/.ethereum/ronin",
	}))

	period := time.NewTicker(3 * time.Second)
	timeout := time.NewTicker(30 * time.Second)

	// check if block is mined
	if err = func() error {
		for {
			select {
			case <-period.C:
				var b block
				t.Log("start getting latest block")
				if err = client1.RPC().Call(&b, "eth_getBlockByNumber", "latest", false); err != nil {
					return err
				}
				t.Log("finished getting block", "number", b.Number, "hash", b.Hash)
				return nil
			case <-timeout.C:
				return errors.New("timed out")
			}
		}
	}(); err != nil {
		t.Fatal("error while trying to get block", "err", err)
	}
	t.Log("start deploying dpos")
	// start calling deploy dpos script
	deployDPOS(t, client1)
}

func deployDPOS(t *hivesim.T, client *hivesim.Client) {
	contractsDirectory, ok := os.LookupEnv("DPOS_DIR")
	if !ok {
		t.Fatal("cannot find DPOS_DIR environment")
	}
	cmd := exec.Command("yarn", "install")
	cmd.Dir = contractsDirectory
	stdout, _ := cmd.StdoutPipe()
	err := cmd.Run()
	scanner := bufio.NewReader(stdout)
	for i := 0; i < scanner.Size(); i++ {
		line, _, _ := scanner.ReadLine()
		t.Log(string(line))
	}
	if err != nil {
		t.Fatal("error while starting install npm", "err", err)
	}

	// create an .env file with privatekey of first validator and rpc of client1
	// create .env file within DPOS dir
	file, err := os.OpenFile(path.Join(contractsDirectory, ".env"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("failed creating file: %s", err.Error())
	}
	datawriter := bufio.NewWriter(file)
	for _, data := range []string{
		fmt.Sprintf("TESTNET_PK=0x%s", validators[0]["privateKey"]),
		fmt.Sprintf("TESTNET_URL=http://%s:8545", client.Container),
	} {
		_, _ = datawriter.WriteString(data + "\n")
	}
	datawriter.Flush()
	file.Close()

	// run deploy script
	cmd = exec.Command("/bin/sh", "./run.sh", "Migration__20231212_DeployTestnet", "-f", "ronin-testnet")
	cmd.Dir = contractsDirectory
	stdout, _ = cmd.StdoutPipe()
	err = cmd.Run()
	scanner = bufio.NewReader(stdout)
	for i := 0; i < scanner.Size(); i++ {
		line, _, _ := scanner.ReadLine()
		t.Log(string(line))
	}
	if err != nil {
		t.Fatal("error while starting run.sh", "err", err)
	}
}
