# xk6-infobip-mcp

**k6 extension for Model Context Protocol (MCP) integration**

This k6 extension enables performance testing of MCP (Model Context Protocol) servers by providing a JavaScript API for creating MCP clients, calling tools, and managing connections. Perfect for load testing MCP-based applications and validating MCP server performance under various conditions.

## Example
```javascript file=script.js
import mcp from "k6/x/infobip_mcp";

export const options = {
  vus: 10,
  duration: '30s',
};

export default function () {
  // Create MCP client
  const client = mcp.NewClient({
    endpoint: "https://your-mcp-server.com/mcp",
    timeout: 30,
    isSSE: false,
    headers: {
      "Authorization": "Bearer your-token",
      "Content-Type": "application/json"
    }
  });

  // Call a tool on the MCP server
  const result = client.CallTool("your_tool_name", {
    param1: "value1",
    param2: 42
  });

  console.log("Tool response:", result);

  // Clean up connection
  client.CloseConnection();
}
```


## Quick Start

1. **Build a custom k6 binary with xk6-infobip-mcp**  
   You need to build k6 with this extension using [xk6](https://github.com/grafana/xk6):

   ```sh
   go install go.k6.io/xk6/cmd/xk6@latest
   xk6 build --with github.com/infobip/xk6-infobip-mcp
   ```

2. **Write your test script**  
   Use the example above or create your own test script `script.js`.

3. **Run your test**  
   Use your custom k6 binary to run the script:

   ```sh
   ./k6 run script.js
   ```

## API Reference

### NewClient(config)

Creates a new MCP client instance.

**Parameters:**
- `config.endpoint` (string): MCP server endpoint URL
- `config.timeout` (number): Connection timeout in seconds used for connection and tool call
- `config.isSSE` (boolean): Use Server-Sent Events transport
- `config.headers` (object, optional): Custom HTTP headers

**Returns:** MCPClient instance

### MCPClient.CallTool(toolName, args)

Calls a tool on the MCP server.

**Parameters:**
- `toolName` (string): Name of the tool to call
- `args` (object): Arguments to pass to the tool

**Returns:** Tool response as string

### MCPClient.CloseConnection()

Closes the MCP client connection.


## Contribute

If you wish to contribute to this project, please start by reading the [Contributing Guidelines](CONTRIBUTING.md).
