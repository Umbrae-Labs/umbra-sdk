use umbra_sdk::{BackupAddress, BackupCategory, UmbraClient};

#[test]
fn config_derives_same_origin_endpoints() {
    let config = UmbraClient::builder()
        .base_url("https://umbra.example.com/")
        .client_id("client")
        .redirect_uri("http://127.0.0.1:0/auth/callback")
        .build()
        .expect("config");

    assert_eq!(config.base_url, "https://umbra.example.com");
    assert!(config.api_base_url.is_none());
    assert!(config.authorization_endpoint.is_none());
    assert!(config.token_endpoint.is_none());
    assert!(config.revocation_endpoint.is_none());
}

#[test]
fn validates_backup_addresses() {
    let valid = [
        BackupAddress::db("v1"),
        BackupAddress::full("2026-05-10T20:00:00"),
        BackupAddress::game("mc", "v1"),
        BackupAddress::asset("cover-mc", "latest"),
    ];
    for address in valid {
        address.validate().expect("valid address");
    }

    let mut invalid = BackupAddress::game("bad/slash", "v1");
    assert!(invalid.validate().is_err());

    invalid = BackupAddress::db("bad space");
    assert!(invalid.validate().is_err());

    assert!(serde_json::from_str::<BackupCategory>("\"sync\"").is_err());
}
