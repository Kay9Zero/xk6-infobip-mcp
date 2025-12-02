import { check } from 'k6';
import mcp from "k6/x/infobip_mcp";

const smsToolArgs = {
  "messages": [
    {
      "sender": "Infobip MCP k6",
      "destinations": [
        {
          "to": `${__ENV.MOBILE_NUMBER}`
        }
      ],
      "content": {
        "text": "Hi! I'm Infobip MCP k6 extension!"
      }
    }
  ]
};

// This will be created once per VU, reused across iterations
let mcpClient;

export default function () {
  // Initialize client only on first iteration for this VU
  if (!mcpClient) {
    mcpClient = mcp.NewClient({
      endpoint: `${__ENV.MCP_SERVER_URL}`,
      isSSE: false,
      timeout: 60,
      headers: {
        "Authorization": `App ${__ENV.API_KEY}`,
        "Content-Type": "application/json",
        "Accept": "application/json, text/event-stream"
      }
    });
  }

  // Reuse the same client for all iterations
  let res = mcpClient.callTool("send_sms_messages", smsToolArgs);
  check(res, {
    'result is not empty': (r) => r !== "",
  });
}

export function teardown() {
  // Close when VU is done
  if (mcpClient) {
    mcpClient.closeConnection();
  }
}
