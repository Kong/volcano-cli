import { VolcanoAuth } from "@volcano.dev/sdk";
import volcanoShared from "../../volcano/_shared/volcano-client";

const { createWebVolcanoClient } = volcanoShared;

let volcanoClient;

export function getVolcanoClient() {
  if (volcanoClient) {
    return volcanoClient;
  }

  volcanoClient = createWebVolcanoClient(VolcanoAuth);
  return volcanoClient;
}
