//! OCI client tests.
//!
//! These tests do not require hypervisor access (except for VM tests).

use cc::{CancelToken, OciClient};

#[test]
fn test_cancel_token() {
    let token = CancelToken::new();

    // New token should not be cancelled
    assert!(
        !token.is_cancelled(),
        "new token should not be cancelled"
    );

    // Cancel the token
    token.cancel();

    // Now it should be cancelled
    assert!(
        token.is_cancelled(),
        "token should be cancelled after cancel()"
    );
}

#[test]
fn test_oci_client_new() {
    cc::init().expect("init should succeed");

    let client = OciClient::new().expect("OciClient::new should succeed");

    // Cache dir should not be empty
    let cache_dir = client.cache_dir();
    assert!(
        !cache_dir.is_empty(),
        "cache_dir should not be empty"
    );
    println!("Cache dir: {}", cache_dir);

    cc::shutdown();
}

#[test]
fn test_oci_client_with_cache_dir() {
    cc::init().expect("init should succeed");

    let temp_dir = std::env::temp_dir().join("cc_rust_test_cache");
    let _ = std::fs::create_dir_all(&temp_dir);

    let client = OciClient::with_cache_dir(temp_dir.to_str().unwrap())
        .expect("OciClient::with_cache_dir should succeed");

    let cache_dir = client.cache_dir();
    assert!(
        cache_dir.contains("cc_rust_test_cache"),
        "cache_dir should contain our custom directory"
    );

    cc::shutdown();
}
