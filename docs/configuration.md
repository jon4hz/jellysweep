#### Statistics app

Either **Jellystat** ^^or^^ **Streamystats** can be used. Simply comment out the other

You will need:

- **URL** (as Jellysweep's container can access it)
- **API key**

<!-- TODO: Streamystats documentation (i don't use streamystats — i'll spin up a container) -->

=== "Jellystat"

    API key:
        
    - `Settings` ➔ `API Key` ➔ `Add Key`
      - Create a new API key. You can name it `Jellysweep`

    ```yaml title="config.yml" linenums="158" hl_lines="1-3"
    jellystat:
        url: "http://localhost:3001"
        api_key: "your-jellystat-api-key"
        timeout: 30                          # HTTP client timeout in seconds (default: 30)
    ```

=== "Streamystats"

    ```yaml title="config.yml" linenums="158" hl_lines="1-3"
    streamystats:
        url: "http://localhost:3001"
        server_id: 1                         # Jellyfin server ID in Streamystats
        timeout: 30                          # HTTP client timeout in seconds (default: 30)
    ```

#### Jellyseerr / Seerr

You will need:

- **URL** (as Jellysweep's container can access it)
- **API key**
    - Jellyseerr: `Settings` ➔ `General` ➔ `API Key`

```yaml title="config.yml" linenums="143"
jellyseerr:
  url: "http://localhost:5055"
  api_key: "your-jellyseerr-api-key"
  timeout: 30                          # HTTP client timeout in seconds (default: 30)
```

<!-- TODO: does Seerr work, too? -->
