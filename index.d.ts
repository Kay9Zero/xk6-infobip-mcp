/**
 * **Infobip MCP k6 extension**
 *
 * A k6 extension for making MCP (Model Context Protocol) tool calls
 *
 * @module infobip_mcp
 */
export as namespace infobip_mcp;

/**
 * Configuration for MCP client connection
 */
export interface ClientConfig {
  /** MCP server endpoint URL */
  endpoint: string;
  /** Connection timeout in seconds */
  timeout: number;
  /** Whether to use Server-Sent Events transport */
  isSSE: boolean;
  /** Custom HTTP headers to include with requests */
  headers?: Record<string, string>;
}

/**
 * MCP Client for making tool calls to MCP servers
 */
export declare class MCPClient {
  /**
   * Close the MCP client connection
   *
   * @throws Error if connection cannot be closed
   */
  CloseConnection(): void;

  /**
   * Call a tool on the MCP server
   *
   * @param toolName Name of the tool to call
   * @param args Arguments to pass to the tool
   * @returns The response from the tool call as a string
   * @throws Error if tool call fails or connection issues occur
   */
  CallTool(toolName: string, args: Record<string, any>): string;
}

/**
 * Create a new MCP client instance
 *
 * @param config Configuration for the MCP client
 * @returns A new MCPClient instance
 */
export declare function NewClient(config: ClientConfig): MCPClient;
