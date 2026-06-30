import { Antenna, Network, Orbit, Repeat2, Route, SlidersHorizontal, Users, type LucideIcon } from "lucide-react";
import type { Section } from "@/lib/routes";

export const SECTION_ICONS: Record<Section, LucideIcon> = {
  operations: Orbit,
  missions: Route,
  routines: Repeat2,
  crews: Network,
  rovers: Antenna,
  members: Users,
  settings: SlidersHorizontal,
};
