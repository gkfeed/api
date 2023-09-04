from fastapi import FastAPI

from . import cors


def setup(app: FastAPI):
    cors.setup(app)
