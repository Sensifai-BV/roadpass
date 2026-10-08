import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import * as Tip from "@radix-ui/react-tooltip";
import { App } from "./App";
import "./styles.css";

const qc = new QueryClient({
  defaultOptions: {
    queries: {
      refetchOnWindowFocus: false,
      retry: (n, err) => n < 2 && !(err instanceof Error && /40[134]/.test(String((err as { status?: number }).status))),
    },
  },
});

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <QueryClientProvider client={qc}>
      <Tip.Provider delayDuration={250}>
        <App />
      </Tip.Provider>
    </QueryClientProvider>
  </StrictMode>,
);
