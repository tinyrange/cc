//! VM integration tests.
//!
//! These tests require hypervisor access and will pull container images.
//! Run with: CC_RUN_VM_TESTS=1 cargo test

use std::env;

fn should_run_vm_tests() -> bool {
    env::var("CC_RUN_VM_TESTS").map(|v| v == "1").unwrap_or(false)
}

fn check_hypervisor() -> bool {
    cc::init().expect("init should succeed");

    let available = match cc::supports_hypervisor() {
        Ok(available) => available,
        Err(_) => false,
    };

    if !available {
        cc::shutdown();
        return false;
    }

    // Try to actually create an instance to verify access
    let client = match cc::OciClient::new() {
        Ok(c) => c,
        Err(_) => {
            cc::shutdown();
            return false;
        }
    };

    let source = match client.pull("alpine:latest", None, None) {
        Ok(s) => s,
        Err(_) => {
            cc::shutdown();
            return false;
        }
    };

    let result = cc::Instance::new(source, None);
    cc::shutdown();

    result.is_ok()
}

#[test]
fn test_pull_image() {
    if !should_run_vm_tests() {
        println!("Skipping VM test (CC_RUN_VM_TESTS not set)");
        return;
    }

    cc::init().expect("init should succeed");

    if !cc::supports_hypervisor().unwrap_or(false) {
        println!("Skipping: hypervisor not available");
        cc::shutdown();
        return;
    }

    let client = cc::OciClient::new().expect("OciClient::new should succeed");
    let source = client
        .pull("alpine:latest", None, None)
        .expect("pull should succeed");

    // Get image config
    let config = source.get_config().expect("get_config should succeed");
    assert!(
        config.architecture.is_some(),
        "architecture should not be None"
    );

    let arch = config.architecture.as_ref().unwrap();
    assert!(
        arch == "amd64" || arch == "arm64" || arch == "x86_64" || arch == "aarch64",
        "unexpected architecture: {}",
        arch
    );

    println!("Image architecture: {}", arch);

    cc::shutdown();
}

#[test]
fn test_create_instance() {
    if !should_run_vm_tests() {
        println!("Skipping VM test (CC_RUN_VM_TESTS not set)");
        return;
    }

    if !check_hypervisor() {
        println!("Skipping: hypervisor not accessible");
        return;
    }

    cc::init().expect("init should succeed");

    let client = cc::OciClient::new().expect("OciClient::new should succeed");
    let source = client
        .pull("alpine:latest", None, None)
        .expect("pull should succeed");

    let opts = cc::InstanceOptions {
        memory_mb: 256,
        cpus: 1,
        ..Default::default()
    };

    let inst = cc::Instance::new(source, Some(opts)).expect("Instance::new should succeed");

    assert!(inst.is_running(), "instance should be running");

    let id = inst.id();
    assert!(!id.is_empty(), "instance ID should not be empty");

    println!("Instance ID: {}", id);

    cc::shutdown();
}

#[test]
fn test_command_execution() {
    if !should_run_vm_tests() {
        println!("Skipping VM test (CC_RUN_VM_TESTS not set)");
        return;
    }

    if !check_hypervisor() {
        println!("Skipping: hypervisor not accessible");
        return;
    }

    cc::init().expect("init should succeed");

    let client = cc::OciClient::new().expect("OciClient::new should succeed");
    let source = client
        .pull("alpine:latest", None, None)
        .expect("pull should succeed");

    let inst = cc::Instance::new(source, None).expect("Instance::new should succeed");

    // Run echo command
    let output = inst
        .command("echo", &["Hello from Rust!"])
        .expect("command should succeed")
        .output()
        .expect("output should succeed");

    let stdout = String::from_utf8_lossy(&output.stdout);
    assert!(
        stdout.contains("Hello from Rust!"),
        "output should contain 'Hello from Rust!', got: {}",
        stdout
    );

    assert_eq!(output.exit_code, 0, "exit code should be 0");

    println!("Command output: {}", stdout.trim());

    cc::shutdown();
}

#[test]
fn test_command_exit_code() {
    if !should_run_vm_tests() {
        println!("Skipping VM test (CC_RUN_VM_TESTS not set)");
        return;
    }

    if !check_hypervisor() {
        println!("Skipping: hypervisor not accessible");
        return;
    }

    cc::init().expect("init should succeed");

    let client = cc::OciClient::new().expect("OciClient::new should succeed");
    let source = client
        .pull("alpine:latest", None, None)
        .expect("pull should succeed");

    let inst = cc::Instance::new(source, None).expect("Instance::new should succeed");

    // Successful command
    let exit_code = inst
        .command("true", &[])
        .expect("command should succeed")
        .run()
        .expect("run should succeed");
    assert_eq!(exit_code, 0, "true should return 0");

    // Failed command
    let exit_code = inst
        .command("false", &[])
        .expect("command should succeed")
        .run()
        .expect("run should succeed");
    assert_ne!(exit_code, 0, "false should return non-zero");

    cc::shutdown();
}

#[test]
fn test_filesystem_write_read() {
    if !should_run_vm_tests() {
        println!("Skipping VM test (CC_RUN_VM_TESTS not set)");
        return;
    }

    if !check_hypervisor() {
        println!("Skipping: hypervisor not accessible");
        return;
    }

    cc::init().expect("init should succeed");

    let client = cc::OciClient::new().expect("OciClient::new should succeed");
    let source = client
        .pull("alpine:latest", None, None)
        .expect("pull should succeed");

    let inst = cc::Instance::new(source, None).expect("Instance::new should succeed");

    let test_path = "/tmp/test_file.txt";
    let test_data = b"Hello, filesystem!";

    // Write file
    inst.write_file(test_path, test_data, 0o644)
        .expect("write_file should succeed");

    // Read file back
    let read_data = inst.read_file(test_path).expect("read_file should succeed");

    assert_eq!(
        read_data, test_data,
        "read data should match written data"
    );

    cc::shutdown();
}

#[test]
fn test_filesystem_stat() {
    if !should_run_vm_tests() {
        println!("Skipping VM test (CC_RUN_VM_TESTS not set)");
        return;
    }

    if !check_hypervisor() {
        println!("Skipping: hypervisor not accessible");
        return;
    }

    cc::init().expect("init should succeed");

    let client = cc::OciClient::new().expect("OciClient::new should succeed");
    let source = client
        .pull("alpine:latest", None, None)
        .expect("pull should succeed");

    let inst = cc::Instance::new(source, None).expect("Instance::new should succeed");

    let test_path = "/tmp/stat_test.txt";
    let test_data = b"Hello, World!";

    inst.write_file(test_path, test_data, 0o644)
        .expect("write_file should succeed");

    let info = inst.stat(test_path).expect("stat should succeed");

    assert_eq!(
        info.size as usize,
        test_data.len(),
        "size should match"
    );
    assert!(!info.is_dir, "should not be a directory");

    cc::shutdown();
}

#[test]
fn test_filesystem_mkdir() {
    if !should_run_vm_tests() {
        println!("Skipping VM test (CC_RUN_VM_TESTS not set)");
        return;
    }

    if !check_hypervisor() {
        println!("Skipping: hypervisor not accessible");
        return;
    }

    cc::init().expect("init should succeed");

    let client = cc::OciClient::new().expect("OciClient::new should succeed");
    let source = client
        .pull("alpine:latest", None, None)
        .expect("pull should succeed");

    let inst = cc::Instance::new(source, None).expect("Instance::new should succeed");

    // Create directory
    inst.mkdir("/tmp/testdir", 0o755)
        .expect("mkdir should succeed");

    // Verify it exists
    let info = inst.stat("/tmp/testdir").expect("stat should succeed");
    assert!(info.is_dir, "should be a directory");

    // List parent directory
    let entries = inst.read_dir("/tmp").expect("read_dir should succeed");
    let names: Vec<&str> = entries.iter().map(|e| e.name.as_str()).collect();
    assert!(
        names.contains(&"testdir"),
        "testdir not found in {:?}",
        names
    );

    cc::shutdown();
}

#[test]
fn test_filesystem_remove() {
    if !should_run_vm_tests() {
        println!("Skipping VM test (CC_RUN_VM_TESTS not set)");
        return;
    }

    if !check_hypervisor() {
        println!("Skipping: hypervisor not accessible");
        return;
    }

    cc::init().expect("init should succeed");

    let client = cc::OciClient::new().expect("OciClient::new should succeed");
    let source = client
        .pull("alpine:latest", None, None)
        .expect("pull should succeed");

    let inst = cc::Instance::new(source, None).expect("Instance::new should succeed");

    let test_path = "/tmp/remove_test.txt";

    // Create file
    inst.write_file(test_path, b"delete me", 0o644)
        .expect("write_file should succeed");

    // Remove it
    inst.remove(test_path).expect("remove should succeed");

    // Verify it's gone
    let result = inst.stat(test_path);
    assert!(result.is_err(), "file should have been removed");

    cc::shutdown();
}

#[test]
fn test_file_handle() {
    if !should_run_vm_tests() {
        println!("Skipping VM test (CC_RUN_VM_TESTS not set)");
        return;
    }

    if !check_hypervisor() {
        println!("Skipping: hypervisor not accessible");
        return;
    }

    cc::init().expect("init should succeed");

    let client = cc::OciClient::new().expect("OciClient::new should succeed");
    let source = client
        .pull("alpine:latest", None, None)
        .expect("pull should succeed");

    let inst = cc::Instance::new(source, None).expect("Instance::new should succeed");

    let test_path = "/tmp/handle_test.txt";

    // Create and write
    {
        let mut f = inst.create(test_path).expect("create should succeed");
        let n = f.write_bytes(b"Hello, World!").expect("write should succeed");
        assert_eq!(n, 13, "should write 13 bytes");
    }

    // Open and read
    {
        let mut f = inst.open(test_path).expect("open should succeed");
        let mut buf = vec![0u8; 5];
        let n = f.read_bytes(&mut buf).expect("read should succeed");
        assert_eq!(n, 5, "should read 5 bytes");
        assert_eq!(&buf[..n], b"Hello", "should read 'Hello'");

        // Seek to beginning
        let pos = f.seek(0, cc::SeekWhence::Set).expect("seek should succeed");
        assert_eq!(pos, 0, "should seek to 0");

        // Read all
        let mut all = Vec::new();
        use std::io::Read;
        f.read_to_end(&mut all).expect("read_to_end should succeed");
        assert_eq!(&all, b"Hello, World!", "should read entire file");
    }

    cc::shutdown();
}
