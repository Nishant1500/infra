"""This is a example+base for making new rabbitmq backends for use in Fates List"""

from lynxfall.rabbit.core import *  # Critical import, this imports a lot of rabbitmq worker functions here

"""
Builtins (in state. Only use stuff in state or logger)

    - stats (main stats class)
    - client (Discord client for main bot)
    - rabbit (RabbitMQ connection)
    - redis (Redis connection)
    - pidrec (PIDRecorder, relies on worker.py)
"""

"""Main config class"""

class Config:
    queue = "queue_name" # Name of the queue that will call this on new task
    name = "Friendly Name" # Firendly name to use, displayed during worker start
    description = "Lorem ipsum..." # Description to use, hidden right now, but may be used in future
    ackall = False # Whether to ack all messages sent
    pre = None # What prehooks to run. These functions must be async, take no arguments and are run during worker start after all builtisn but stats and ones not in process.py (pidrec) is loaded

async def backend(state, json, *args, **kwargs):
    pass
