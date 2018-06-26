package driver

//
//  three steps to mocking a single os/exec.Command call

// func TestGetMappedDevice(t *testing.T) {
// 	execCommand = fakeExecLsblk
// 	defer func() { execCommand = exec.Command }()

// 	blockInfo, err := getMappedDevice("md126")
// 	assert.Nil(t, err)
// 	assert.NotNil(t, blockInfo)
// 	assert.Equal(t, "md126", blockInfo.Name)
// 	assert.Equal(t, "ext4", blockInfo.FsType)
// }

// func TestMockOfLsblk(*testing.T) {
// 	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
// 		return
// 	}
// 	defer os.Exit(0)

// 	lsblkStdout := `{"blockdevices":[{"name":"md126","fstype":"ext4","label":"ROOT","uuid":"afbc32b3-d258-4553-bd1b-4da06768c63f","mountpoint":"/"}]}`
// 	fmt.Println(lsblkStdout)
// }

// func fakeExecLsblk(command string, args ...string) *exec.Cmd {
// 	cs := []string{"-test.run=TestMockOfLsblk", "--", command}
// 	cs = append(cs, args...)
// 	cmd := exec.Command(os.Args[0], cs...)
// 	cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1"}
// 	return cmd
// }
