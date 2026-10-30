import { useEffect } from "react";
import { setRealtimePreference } from "@/lib/api";
import { useUIStore } from "@/stores/ui.store";

let realtimePreferenceTransition = Promise.resolve();

function enqueueRealtimePreference(mode: "realtime" | "balanced" | "battery" | "manual") {
  const transition = realtimePreferenceTransition
    .catch(() => {})
    .then(() => setRealtimePreference(mode));
  realtimePreferenceTransition = transition;
  void transition.catch(() => {});
}

export function useRealtimePreferenceSync() {
  const realtimeMode = useUIStore((state) => state.realtimeMode);

  useEffect(() => {
    enqueueRealtimePreference(realtimeMode);
  }, [realtimeMode]);
}
