@cli @sign
Feature: CLI Sign Command
  The `gtb sign` command produces an ASCII-armored OpenPGP detached
  signature over a file using a configured signing backend. The private
  key never leaves the backend, and the supplied public key is the
  identity of record — it must match the backend-resolved key or the
  command refuses to proceed.

  These scenarios exercise the operator-visible workflow: generate a
  keypair, sign a file with the local backend, and the key-mismatch and
  missing-flag error paths. Cryptographic correctness (algorithms,
  fingerprints) is covered by the unit suites in pkg/signing and
  internal/cmd/sign.

  Background:
    Given the gtb binary is built
    And a temporary keys directory

  @smoke
  Scenario: Sign a file with the local backend produces a detached signature
    Given I run gtb with "keys generate --algorithm rsa --rsa-bits 2048 --name Test --email test@example.org --output {keys_dir}/release.asc --private-output {keys_dir}/release.pem"
    When I run gtb with "sign {keys_dir}/release.asc --backend local --key-id {keys_dir}/release.pem --public-key {keys_dir}/release.asc --output {keys_dir}/release.asc.sig"
    Then the exit code is 0
    And the file "release.asc.sig" exists in the keys directory
    And the file "release.asc.sig" in the keys directory contains "-----BEGIN PGP SIGNATURE-----"

  Scenario: Sign fails when no backend is given
    When I run gtb with "sign {keys_dir}/release.asc"
    Then the exit code is not 0

  Scenario: Sign fails with an unknown backend
    When I run gtb with "sign {keys_dir}/release.asc --backend not-a-backend --key-id x --public-key y"
    Then the exit code is not 0

  Scenario: Sign refuses a public key that does not match the backend key
    Given I run gtb with "keys generate --algorithm rsa --rsa-bits 2048 --name A --email a@example.org --output {keys_dir}/a.asc --private-output {keys_dir}/a.pem"
    And I run gtb with "keys generate --algorithm rsa --rsa-bits 2048 --name B --email b@example.org --output {keys_dir}/b.asc --private-output {keys_dir}/b.pem"
    When I run gtb with "sign {keys_dir}/a.asc --backend local --key-id {keys_dir}/a.pem --public-key {keys_dir}/b.asc --output {keys_dir}/mismatch.sig"
    Then the exit code is not 0
