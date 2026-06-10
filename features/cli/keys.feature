@cli @keys
Feature: CLI Keys Command Group
  The `gtb keys` command group handles release-binary signing key
  lifecycle — locally generating an OpenPGP keypair, minting an OpenPGP
  public half from a backend-held signer (KMS or local PEM), and
  publishing the public half via a Web Key Directory tree.

  These scenarios exercise the operator-visible behaviour:
  flag handling, file outputs, OpenPGP framing, and the most
  meaningful error paths. Cryptographic correctness (signature
  algorithms, fingerprint reproducibility, etc.) is covered by the
  unit suites in pkg/openpgpkey and internal/cmd/keys.

  Background:
    Given the gtb binary is built
    And a temporary keys directory

  # ----- `gtb keys generate` ------------------------------------------

  @smoke
  Scenario: Generate an Ed25519 keypair produces armored both-halves
    When I run gtb with "keys generate --algorithm ed25519 --name Test --email test@example.org --output {keys_dir}/rotation.asc --private-output {keys_dir}/rotation.priv.asc"
    Then the exit code is 0
    And stderr contains "Generated OpenPGP keypair"
    And stderr contains "algorithm=ed25519"
    And the file "rotation.asc" exists in the keys directory
    And the file "rotation.priv.asc" exists in the keys directory
    And the file "rotation.asc" in the keys directory contains "-----BEGIN PGP PUBLIC KEY BLOCK-----"
    And the file "rotation.priv.asc" in the keys directory contains "-----BEGIN PGP PRIVATE KEY BLOCK-----"

  @smoke
  Scenario: Generate an RSA keypair produces armored public + PKCS#1 PEM private
    When I run gtb with "keys generate --algorithm rsa --rsa-bits 2048 --name Test --email test@example.org --output {keys_dir}/release.asc --private-output {keys_dir}/release.pem"
    Then the exit code is 0
    And the file "release.asc" in the keys directory contains "-----BEGIN PGP PUBLIC KEY BLOCK-----"
    And the file "release.pem" in the keys directory contains "-----BEGIN RSA PRIVATE KEY-----"

  Scenario: Generate fails when a required flag is missing
    When I run gtb with "keys generate --algorithm ed25519 --email test@example.org --output {keys_dir}/x.asc --private-output {keys_dir}/x.priv.asc"
    Then the exit code is not 0
    And stderr contains "name"

  Scenario: Generate rejects an unknown algorithm
    When I run gtb with "keys generate --algorithm magic --name Test --email test@example.org --output {keys_dir}/x.asc --private-output {keys_dir}/x.priv.asc"
    Then the exit code is not 0
    And stderr contains "unknown algorithm"

  # ----- `gtb keys mint` ----------------------------------------------

  @smoke
  Scenario: Mint with the local backend wraps a PEM key in OpenPGP framing
    Given an RSA keypair has been generated as "release"
    When I run gtb with "keys mint --backend local --key-id {keys_dir}/release.pem --name Test --email test@example.org --output {keys_dir}/mint.asc"
    Then the exit code is 0
    And stderr contains "Minted OpenPGP key"
    And the file "mint.asc" exists in the keys directory
    And the file "mint.asc" in the keys directory contains "-----BEGIN PGP PUBLIC KEY BLOCK-----"

  Scenario: Mint refuses an unknown backend with a clear error
    Given an RSA keypair has been generated as "release"
    When I run gtb with "keys mint --backend nonexistent --key-id {keys_dir}/release.pem --name Test --email test@example.org --output {keys_dir}/x.asc"
    Then the exit code is not 0
    And stderr contains "nonexistent"
    And stderr contains "available"

  Scenario: Mint requires a backend flag
    Given an RSA keypair has been generated as "release"
    When I run gtb with "keys mint --key-id {keys_dir}/release.pem --name Test --email test@example.org --output {keys_dir}/x.asc"
    Then the exit code is not 0
    And stderr contains "backend"

  # ----- `gtb keys wkd` -----------------------------------------------

  @smoke
  Scenario: WKD produces the canonical advanced-method layout
    Given an RSA keypair has been generated as "release"
    When I run gtb with "keys wkd --domain example.org --email test@example.org --output {keys_dir}/wkd-staging {keys_dir}/release.asc"
    Then the exit code is 0
    And stderr contains "WKD tree complete"
    And the file "wkd-staging/.well-known/openpgpkey/example.org/policy" exists in the keys directory
    And the file "wkd-staging/.well-known/openpgpkey/example.org/hu/iffe93qcsgp4c8ncbb378rxjo6cn9q6u" exists in the keys directory

  Scenario: WKD requires a domain flag
    Given an RSA keypair has been generated as "release"
    When I run gtb with "keys wkd --email test@example.org --output {keys_dir}/wkd-staging {keys_dir}/release.asc"
    Then the exit code is not 0
    And stderr contains "domain"

  Scenario: WKD errors when an --email has no matching key UID
    Given an RSA keypair has been generated as "release"
    When I run gtb with "keys wkd --domain example.org --email unrelated@example.org --output {keys_dir}/wkd-staging {keys_dir}/release.asc"
    Then the exit code is not 0
    And stderr contains "unrelated@example.org"
