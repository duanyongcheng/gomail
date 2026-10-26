import { runtime } from "mygo-runtime";
export const getVersion = () => Promise.resolve(runtime().version);
