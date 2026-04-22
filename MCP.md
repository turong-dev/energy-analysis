# MCP Server Configuration

The energy-utility server includes a [Model Context Protocol (MCP)](https://modelcontextprotocol.io) server for exposing energy data to LLMs.

## Overview

The MCP server provides two interfaces:
- **Resources**: Read-only access to energy data via URI scheme
- **Tools**: Executable functions for querying data

## Configuration

Add MCP configuration to your `config.yaml`:

```yaml
mcp:
  enabled: true                    # Enable MCP server
  path: "/mcp"                     # Base path for MCP endpoints
  auto_discover: true              # Auto-discover devices/tariffs from S3
```

### Auto-Discovery

When `auto_discover: true`, the server will:
1. Scan S3 storage for existing data
2. Create/update device and tariff registries in `registry/devices.json` and `registry/tariffs.json`
3. Use these registries to populate MCP resources

The harvesters (`fetch_solax` and `fetch_octopus`) automatically update the registries when they fetch new data.

### Manual Configuration (Optional)

If auto-discovery doesn't find everything, you can manually specify:

```yaml
mcp:
  enabled: true
  path: "/mcp"
  auto_discover: true
  
  # Optional: Add devices that weren't auto-discovered
  devices:
    - site_id: "home"
      device_id: "inverter-1"
      type: "solax"
  
  # Optional: Add tariffs that weren't auto-discovered
  tariffs:
    import:
      - id: "my-custom-tariff"
        name: "My Custom Tariff"
        code: "E-1R-..."
        type: "dynamic"
    export: []
```

## Endpoints

When enabled, the MCP server exposes:

- `GET /mcp/sse` - SSE endpoint for MCP sessions
- `POST /mcp/message?session_id={id}` - Message endpoint

## Resources

### Device Resources

| URI Pattern | Description |
|-------------|-------------|
| `energy://sites` | List available sites |
| `energy://site/{site}/devices` | List devices at a site |
| `energy://site/{site}/device/{device}/day/{YYYY-MM-DD}` | Single day data |
| `energy://site/{site}/device/{device}/range/{from}/{to}` | Date range data |
| `energy://site/{site}/device/{device}/meta` | Device metadata |

### Tariff Resources

| URI Pattern | Description |
|-------------|-------------|
| `energy://tariffs/import` | List import tariff IDs |
| `energy://tariffs/export` | List export tariff IDs |
| `energy://tariff/import/{id}/rates/{from}/{to}` | Import rates for date range |
| `energy://tariff/export/{id}/rates/{from}/{to}` | Export rates for date range |
| `energy://tariff/import/{id}/agreement` | Import tariff metadata |
| `energy://tariff/export/{id}/agreement` | Export tariff metadata |

## Tools

| Tool | Description |
|------|-------------|
| `get_device_day` | Fetch device data for a specific day |
| `get_device_range` | Fetch device data for a date range |
| `get_import_rates` | Fetch import tariff rates |
| `get_export_rates` | Fetch export tariff rates |
| `list_sites` | List all registered sites |
| `list_devices` | List devices at a site |
| `list_import_tariffs` | List available import tariffs |
| `list_export_tariffs` | List available export tariffs |

## Connecting with Claude Desktop

To connect Claude Desktop to your MCP server:

1. **Install Claude Desktop** from https://claude.ai/download

2. **Edit the configuration file**:
   - macOS: `~/Library/Application Support/Claude/claude_desktop_config.json`
   - Windows: `%APPDATA%/Claude/claude_desktop_config.json`

3. **Add your MCP server**:

```json
{
  "mcpServers": {
    "energy-utility": {
      "url": "http://localhost:8080/mcp/sse"
    }
  }
}
```

4. **Restart Claude Desktop**

5. **Verify connection**: Look for the MCP server indicator in the bottom-right corner of Claude Desktop. You should see "energy-utility" listed.

## Example Queries

Once connected, you can ask Claude questions like:

- "What was my solar generation on April 15th?"
- "Show me my import rates for this week"
- "Compare my energy costs between March and April"
- "When were the cheapest import rates last month?"
- "How much did I export to the grid yesterday?"

## Testing with MCP Inspector

The [MCP Inspector](https://github.com/modelcontextprotocol/inspector) is a useful tool for debugging:

```bash
npx @modelcontextprotocol/inspector http://localhost:8080/mcp/sse
```

This opens a web UI where you can browse resources and invoke tools.

## Security Considerations

- The MCP server currently has no authentication - it trusts the local network
- For production use, consider:
  - Running behind a reverse proxy with authentication
  - Using HTTPS for the SSE endpoint
  - Limiting access to localhost or VPN-only

## Troubleshooting

### Connection Refused

- Verify the server is running: `curl http://localhost:8080/api/rates?from=2025-04-01&to=2025-04-02`
- Check the MCP path in config matches your client configuration

### No Resources Listed

- Check that devices/tariffs are configured in `config.yaml`
- Verify data exists in S3 storage
- Check server logs for registration messages

### Empty Data Responses

- Verify S3 data exists for the requested dates
- Check that the cache directory is writable (if using caching)
- Review server logs for errors fetching data

### Registry Not Found

If you see "could not load device/tariff registry" messages:
- This is normal on first run - the server will auto-discover from storage
- Run the harvesters to populate data and registries:
  ```bash
  go run ./cmd/harvest --config config.yaml
  ```
- Or manually trigger discovery by restarting the server after data exists in S3

## Registry Structure

The MCP server uses JSON registries stored in S3 to track devices and tariffs:

### Device Registry (`registry/devices.json`)

```json
{
  "version": "1.0",
  "updated_at": "2025-04-18T10:00:00Z",
  "devices": [
    {
      "site_id": "home",
      "device_id": "inverter-1",
      "type": "solax",
      "manufacturer": "SolaX",
      "model": "X3-Hybrid",
      "data_source": {
        "type": "s3",
        "path": "solax/raw/",
        "format": "daily-detail"
      },
      "first_data": "2025-04-01T00:00:00Z",
      "last_data": "2025-04-18T00:00:00Z",
      "data_count": 18
    }
  ]
}
```

### Tariff Registry (`registry/tariffs.json`)

```json
{
  "version": "1.0",
  "updated_at": "2025-04-18T10:00:00Z",
  "import": [
    {
      "id": "agile-import",
      "name": "Octopus Agile Import",
      "code": "E-1R-AGILE-24-10-01-C",
      "type": "dynamic",
      "direction": "import",
      "provider": "octopus",
      "data_source": {
        "type": "s3",
        "path": "octopus/agile-import/",
        "format": "monthly-rates"
      },
      "active_from": "2025-04-01T00:00:00Z"
    }
  ],
  "export": [...]
}
```

These registries are automatically created and updated by the harvesters.

## Data Flow

```
┌─────────────────┐     HTTP      ┌──────────────┐
│  MCP Client     │ ◄────────────► │  MCP Server  │
│  (Claude/etc)   │    SSE/POST    │  (/mcp/...)  │
└─────────────────┘                └──────┬───────┘
                                          │
                         ┌────────────────┼────────────────┐
                         │                │                │
                    ┌────▼────┐     ┌─────▼─────┐   ┌──────▼──────┐
                    │  SolaX  │     │  Octopus  │   │   Cache     │
                    │  (S3)   │     │   (S3)    │   │  (local)    │
                    └─────────┘     └───────────┘   └─────────────┘
```

The MCP server reads device data from S3 (SolaX daily-detail files) and tariff data from S3 (Octopus Agile rates). It uses the configured cache directory for local caching.
