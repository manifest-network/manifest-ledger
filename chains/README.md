# Local Interchain Testnets

- Install [local-interchain](https://github.com/strangelove-ventures/interchaintest/tree/main/local-interchain). (or download from the releases page)
- In this project, `make local-image` (creates local docker image)
- `ICTEST_HOME=. local-ic start chain`

## Other
You can distribute a docker file with the following command
```bash
docker image save manifest:local > manifest_local.tar
```

Then load it in with
```bash
docker image load -i manifest_local.tar
```