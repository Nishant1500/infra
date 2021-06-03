from rabbitmq.core import *
import asyncio
import orjson
import builtins
import time
from modules.core import *

class PIDRecorder():
    def __init__(self):
        self.pids = []

    def record(self, pid):
        if pid in self.pids:
            return
        self.pids.append(pid)
        self.pids.sort()

    def remove(self, pid):
        try:
            pid_index = self.pids.index(pid)
        except ValueError:
            return
        del self.pids[pid_index]

    def worker_amt(self):
        return len(self.pids)

    def reset(self):
        self.pids = []

    def list(self):
        return self.pids

async def status(pidrec):
    pubsub = redis_db.pubsub()
    await pubsub.subscribe(f"{instance_name}._worker")
    flag = True
    async for msg in pubsub.listen():
        if flag:
            await redis_db.publish(f"{instance_name}._worker", "UP RMQ 0") # Announce that we are up
            flag = False
        print(msg)
        if msg is None or type(msg.get("data")) != bytes:
            continue
        msg = tuple(msg.get("data").decode("utf-8").split(" "))
        match msg:
            case ("UP", ("RMQ" | "WORKER") as tgt, pid, reload, worker_amt) if pid.isdigit() and reload.isdigit() and worker_amt.isdigit():
                logger.info(f"{tgt} {pid} is now up with reload mode {reload}. Amount of workers is {worker_amt}")
                pidrec.record(int(pid)) if tgt == "WORKER" else None
                if pidrec.worker_amt() > int(worker_amt) and tgt == "WORKER":
                    logger.warning(f"Invalid worker {worker_amt} with pid {pid} added. Restting config and publishing REGET")
                    pidrec.reset()
                    await asyncio.sleep(1)
                    await redis_db.publish(f"{instance_name}._worker", "REGET WORKER INVALID_STATE")

            case ("DOWN", ("RMQ" | "WORKER") as tgt, pid) if pid.isdigit():
                logger.info(f"{tgt} {pid} is now down")
                pidrec.remove(int(pid)) if tgt == "RMQ" else None
            case _:
                logger.warning(f"Unhandled message {msg}")

async def lock_unlock():
    if playground:
        return # Playground doesnt get this
    while True:
        try:
            # Handle Staff Requests
            req = await redis_db.get("fl_staff_req")
            req = orjson.loads(req) if req else None
            if req and isinstance(req, list):
                for i in range(len(req)):
                    try:
                        r = req[i]
                    except:
                        continue # Already deleted

                    if r["op"] not in ("lock", "unlock"):
                        continue
                    logger.info(f"Got {r['op']} request")
                    user = await get_user(int(r["staff"]))
                    if not user:
                        continue
                    key = "lock-" + str(r["staff"]) + "-" + str(r["bot_id"])
                    sa = await redis_db.get("staff_access")
                    sa = orjson.loads(sa) if sa else []
                    sa.append({"staff": r["staff"], "bot_id": r["bot_id"], "key": key, "time": time.time()})
                    await redis_db.set("staff_access", orjson.dumps(sa))
                    await redis_db.set(key, orjson.dumps(r["op"] == "unlock"), ex = 60*16) # Give one minute for us to handle staff_access
                    await client.wait_until_ready()
                    channel = client.get_channel(bot_logs)
                    bot = await get_bot(r["bot_id"])
                    if not bot:
                        bot = {"username": "Unknown", "disc": "0000"}
                    embed = discord.Embed(title = "Staff Access Alert!", description = f"Staff member {user['username']}#{user['disc']} have {r['op'] + 'ed'} {bot['username']}#{bot['disc']} for editing. This is normal but if it happens too much, open a ticket or contact any online or offline staff immediately")
                    await channel.send(embed = embed)
                    del req[i]
                    await redis_db.set("fl_staff_req", orjson.dumps(req)) 
        except Exception:
            logger.exception("Something happened!")
        await asyncio.sleep(5)

async def prehook(*args, **kwargs):
    builtins.pidrec = PIDRecorder()
    asyncio.create_task(status(pidrec))
    asyncio.create_task(lock_unlock())

async def backend(json, *args, **kwargs):
    return pidrec.list()

class Config:   
    queue = "_worker"
    name = "Worker Handler" 
    descriprion = "Handle Workers (PIDs right now but may be increased in future)"
    pre = prehook
