// Server-side proxy to the Go backend. Its only job is to hold the bearer
// token: the browser talks to this route, this route talks to the API. That
// also means the frontend needs no CORS configuration and no per-user token
// handling until real SSO lands.

import { NextRequest } from "next/server";

const backend = process.env.BENCH_API_URL ?? "http://127.0.0.1:8080";
const token = process.env.BENCH_API_TOKEN ?? "";

export const dynamic = "force-dynamic";

async function proxy(request: NextRequest, path: string[]): Promise<Response> {
  const target = new URL(`/api/v1/${path.join("/")}`, backend);
  target.search = request.nextUrl.search;

  const headers = new Headers();
  headers.set("Accept", "application/json");
  if (token) {
    headers.set("Authorization", `Bearer ${token}`);
  }
  const contentType = request.headers.get("Content-Type");
  if (contentType) {
    headers.set("Content-Type", contentType);
  }

  let body: string | undefined;
  if (request.method !== "GET" && request.method !== "HEAD") {
    body = await request.text();
  }

  let response: Response;
  try {
    response = await fetch(target, {
      method: request.method,
      headers,
      body,
      cache: "no-store",
    });
  } catch (error) {
    // A backend that is down is by far the most common failure while
    // developing, and a bare fetch failure in the browser console says
    // nothing useful about which URL was unreachable.
    return Response.json(
      { error: `cannot reach the benchmark API at ${backend}: ${(error as Error).message}` },
      { status: 502 },
    );
  }

  return new Response(response.body, {
    status: response.status,
    headers: { "Content-Type": response.headers.get("Content-Type") ?? "application/json" },
  });
}

type Context = { params: Promise<{ path: string[] }> };

export async function GET(request: NextRequest, context: Context) {
  return proxy(request, (await context.params).path);
}

export async function POST(request: NextRequest, context: Context) {
  return proxy(request, (await context.params).path);
}

export async function PUT(request: NextRequest, context: Context) {
  return proxy(request, (await context.params).path);
}

export async function DELETE(request: NextRequest, context: Context) {
  return proxy(request, (await context.params).path);
}
