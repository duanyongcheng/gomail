import { on } from "mygo-runtime";
export async function listen<T>(name: string, listener: (event: { payload: T }) => void) {
  return on<T>(name, (payload) => listener({ payload }));
}
