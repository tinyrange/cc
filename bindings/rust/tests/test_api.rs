//! API version and library initialization tests.
//!
//! These tests do not require hypervisor access.

use cc;

#[test]
fn test_api_version() {
    let version = cc::api_version();
    assert_eq!(version, "0.1.0", "Expected API version 0.1.0, got {}", version);
}

#[test]
fn test_api_version_compatible() {
    // Compatible versions
    assert!(
        cc::api_version_compatible(0, 1),
        "0.1 should be compatible"
    );
    assert!(
        cc::api_version_compatible(0, 0),
        "0.0 should be compatible"
    );

    // Incompatible versions
    assert!(
        !cc::api_version_compatible(1, 0),
        "1.0 should NOT be compatible"
    );
    assert!(
        !cc::api_version_compatible(0, 99),
        "0.99 should NOT be compatible"
    );
}

#[test]
fn test_init_shutdown() {
    cc::init().expect("init should succeed");
    cc::shutdown();
}

#[test]
fn test_guest_protocol_version() {
    cc::init().expect("init should succeed");
    let version = cc::guest_protocol_version();
    assert_eq!(version, 1, "Expected guest protocol version 1, got {}", version);
    cc::shutdown();
}

#[test]
fn test_supports_hypervisor() {
    cc::init().expect("init should succeed");
    // This should not panic regardless of whether hypervisor is available
    let result = cc::supports_hypervisor();
    assert!(result.is_ok(), "supports_hypervisor should return Ok");
    cc::shutdown();
}

#[test]
fn test_query_capabilities() {
    cc::init().expect("init should succeed");
    let caps = cc::query_capabilities().expect("query_capabilities should succeed");

    // Architecture should be non-empty
    assert!(
        !caps.architecture.is_empty(),
        "architecture should not be empty"
    );

    // Should be one of the known architectures
    assert!(
        caps.architecture == "x86_64"
            || caps.architecture == "amd64"
            || caps.architecture == "arm64"
            || caps.architecture == "aarch64"
            || caps.architecture == "riscv64",
        "unexpected architecture: {}",
        caps.architecture
    );

    println!(
        "Capabilities: hypervisor={}, arch={}, max_memory={}MB, max_cpus={}",
        caps.hypervisor_available, caps.architecture, caps.max_memory_mb, caps.max_cpus
    );

    cc::shutdown();
}
