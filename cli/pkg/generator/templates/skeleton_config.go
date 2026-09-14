package templates

const SkeletonConfig = `log:
  level: info

server:
  grpc:
    reflection: true

# Self-update behaviour. The tool's compiled-in baselines (set at generation
# time via props.Tool.UpdatePolicy / props.Tool.UpdateCheckInterval) apply
# unless overridden here.
update:
  # Posture when a newer release is found: disabled (log only), prompt
  # (ask to update; declining continues), or enabled (block until updated).
  # Empty uses the tool's compiled-in baseline.
  policy: ""
  # How often to check for updates (any Go duration, e.g. 24h, 168h).
  # "0" checks on every invocation. Empty uses the tool's baseline (default 24h).
  check_interval: ""
`
