//! Basic example demonstrating cc virtualization library usage.
//!
//! Run with: cargo run --example basic

use cc::{Instance, InstanceOptions, OciClient};

fn main() -> cc::Result<()> {
    // Initialize the library
    cc::init()?;

    println!("API Version: {}", cc::api_version());
    println!(
        "Version compatible with 0.1: {}",
        cc::api_version_compatible(0, 1)
    );
    println!("Guest protocol version: {}", cc::guest_protocol_version());

    // Query system capabilities
    let caps = cc::query_capabilities()?;
    println!("\nSystem capabilities:");
    println!("  Architecture: {}", caps.architecture);
    println!("  Hypervisor available: {}", caps.hypervisor_available);
    if caps.max_memory_mb > 0 {
        println!("  Max memory: {} MB", caps.max_memory_mb);
    }
    if caps.max_cpus > 0 {
        println!("  Max CPUs: {}", caps.max_cpus);
    }

    // Check hypervisor support
    if !cc::supports_hypervisor()? {
        println!("\nHypervisor not available - skipping VM example");
        cc::shutdown();
        return Ok(());
    }

    println!("\n--- Creating OCI Client ---");
    let client = OciClient::new()?;
    println!("Cache directory: {}", client.cache_dir());

    println!("\n--- Pulling alpine:latest ---");
    let source = client.pull("alpine:latest", None, None)?;

    // Get image config
    let config = source.get_config()?;
    println!("Image architecture: {:?}", config.architecture);
    println!("Image entrypoint: {:?}", config.entrypoint);
    println!("Image cmd: {:?}", config.cmd);

    println!("\n--- Creating Instance ---");
    let opts = InstanceOptions {
        memory_mb: 256,
        cpus: 1,
        ..Default::default()
    };

    let inst = match Instance::new(source, Some(opts)) {
        Ok(i) => i,
        Err(cc::Error::HypervisorUnavailable(msg)) => {
            println!("Hypervisor access denied: {}", msg);
            println!("This may be due to missing entitlements.");
            cc::shutdown();
            return Ok(());
        }
        Err(e) => return Err(e),
    };

    println!("Instance ID: {}", inst.id());
    println!("Instance running: {}", inst.is_running());

    println!("\n--- Running Commands ---");

    // Echo command
    let output = inst.command("echo", &["Hello from Rust!"])?.output()?;
    println!(
        "echo output: {}",
        String::from_utf8_lossy(&output.stdout).trim()
    );
    println!("exit code: {}", output.exit_code);

    // List root directory
    let output = inst.command("ls", &["-la", "/"])?.output()?;
    println!("\nls -la /:");
    for line in String::from_utf8_lossy(&output.stdout).lines().take(5) {
        println!("  {}", line);
    }
    println!("  ...");

    println!("\n--- Filesystem Operations ---");

    // Write a file
    let test_path = "/tmp/rust_test.txt";
    let test_data = b"Hello from Rust bindings!";
    inst.write_file(test_path, test_data, 0o644)?;
    println!("Wrote {} bytes to {}", test_data.len(), test_path);

    // Read it back
    let read_data = inst.read_file(test_path)?;
    println!("Read: {}", String::from_utf8_lossy(&read_data));

    // Get file info
    let info = inst.stat(test_path)?;
    println!(
        "File info: name={}, size={}, is_dir={}",
        info.name, info.size, info.is_dir
    );

    // Create a directory
    inst.mkdir("/tmp/rust_test_dir", 0o755)?;
    println!("Created directory /tmp/rust_test_dir");

    // List /tmp
    let entries = inst.read_dir("/tmp")?;
    println!("\nContents of /tmp:");
    for entry in entries.iter().take(10) {
        println!(
            "  {} (dir={})",
            entry.name, entry.is_dir
        );
    }

    // Cleanup
    inst.remove(test_path)?;
    inst.remove_all("/tmp/rust_test_dir")?;
    println!("\nCleaned up test files");

    // Instance is automatically closed on drop
    println!("\n--- Done ---");

    cc::shutdown();
    Ok(())
}
