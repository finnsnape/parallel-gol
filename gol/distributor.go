package gol

import (
	"strconv"
	"sync"
	"uk.ac.bris.cs/gameoflife/util"
)

type distributorChannels struct {
	events     chan<- Event
	ioCommand  chan<- ioCommand
	ioIdle     <-chan bool
	ioFilename chan<- string
	ioOutput   chan<- uint8
	ioInput    <-chan uint8
}

// Board stores one game of life board and its width/height
type Board struct {
	cells [][]uint8
	width int
	height int
}

// BoardStates stores the current state of the board, the state of the board if we make one more turn, and the w/h
type BoardStates struct {
	current *Board
	next *Board
	width int
	height int
}

// Create a board struct given a width and height
// Note we create the columns first, so we need to do cells[y][x]
func createBoard(width int, height int) *Board {
	cells := make([][]uint8, height)
	for x := range cells {
		cells[x] = make([]uint8, width)
	}
	return &Board{cells: cells, width: width, height: height}
}

// Creates the board states given a width, height and the input
func createBoardStates(width int, height int, c distributorChannels) *BoardStates {
	current := createBoard(width, height)
	current.PopulateBoard(c) // set the cells of the current board to those from the input
	next := createBoard(width, height)
	return &BoardStates{current: current, next: next, width: width, height: height}
}

// PopulateBoard sets the board values to the input
func (board *Board) PopulateBoard(c distributorChannels) {
	for j:=0; j<board.height; j++ {
		for i:=0; i<board.width; i++ {
			board.Set(i, j, <- c.ioInput)
		}
	}
}

// Get gets the value from a cell
func (board *Board) Get (x int, y int) uint8 {
	return board.cells[y][x]
}

// Set sets the value of a cell
func (board *Board) Set (x int, y int, val uint8) {
	board.cells[y][x] = val
}

// CheckAlive checks if a cell is alive, accounting for wrap around if necessary
func (board *Board) CheckAlive (x int, y int, wrap bool) bool {
	if wrap {
		x = (x + board.width) % board.width // need to add the width/height for these as Go modulus doesn't like negatives
		y = (y + board.height) % board.height
	}
	return board.Get(x, y) == 255
}

// AdvanceCell returns the new value for a specific cell after a turn
// Checks every cell within 1 of the given cell, and then checks if each of these are alive to get the neighbour count
func (board *Board) AdvanceCell(x int, y int) uint8 {
	aliveNeighbours := 0
	for i:=-1; i<=1; i++ {
		for j:=-1; j<=1; j++ {
			if i == 0 && j == 0 { // ensure we aren't counting the cell itself
				continue
			}
			if board.CheckAlive(x+j, y+i, true) {
				aliveNeighbours++
			}
		}
	}
	if board.CheckAlive(x,y, false) { // if the cell is alive
		if aliveNeighbours < 2 || aliveNeighbours > 3 {
			return 0 // dies
		} else {
			return 255 // stays the same
		}
	} else { // if the cell is dead
		if aliveNeighbours == 3 {
			return 255 // becomes alive
		} else {
			return 0 // stays the same
		}
	}
}

// AdvanceBoardStates moves the game forward by one turn
func (boardStates *BoardStates) AdvanceBoardStates(startX int, endX int, startY int, endY int) {
	for j:=startY; j<endY; j++ {
		for i:=startX; i<endX; i++ {
			boardStates.next.Set(i, j, boardStates.current.AdvanceCell(i, j))
		}
	}
}

// Update boardStates.next based on boardStates.current, from startY up to endY
func worker(wg *sync.WaitGroup, boardStates *BoardStates, startX int, endX int, startY int, endY int) {
	defer wg.Done()
	boardStates.AdvanceBoardStates(startX, endX, startY, endY)
}

// Splits the board into horizontal slices, and each worker works on one section
func (boardStates *BoardStates) splitWorkers(wg *sync.WaitGroup, workers int, width int, height int) {
	var startX, endX, startY, endY int
	for i:=0; i<workers; i++ {
		startX = 0
		endX = width
		startY = i * height/workers
		if i == workers-1 {
			endY = height
		} else {
			endY = (i + 1) * height/workers
		}
		wg.Add(1)
		go worker(wg, boardStates, startX, endX, startY, endY)
	}
}

// FinalAliveCells returns a list of cells that are alive once the game has finished
func (board *Board) FinalAliveCells() []util.Cell {
	var aliveCells []util.Cell
	for j:=0; j<board.height; j++ {
		for i:=0; i<board.width; i++ {
			if board.CheckAlive(i, j, false) {
				aliveCells = append(aliveCells, util.Cell{X: i, Y: j})
			}
		}
	}
	return aliveCells
}

// distributor divides the work between workers and interacts with other goroutines.
func distributor(p Params, c distributorChannels) {
	// make the filename and pass it through channel
	var filename string
	height := p.ImageHeight
	width := p.ImageWidth
	filename = strconv.Itoa(width)  + "x" + strconv.Itoa(height)
	c.ioCommand <- ioInput // start read the image
	c.ioFilename <- filename // pass the filename of the image

	boardStates := createBoardStates(width, height, c)

	var wg sync.WaitGroup
	workers := p.Threads
	turn := 0
	for ; turn<p.Turns; turn++ { // execute the turns
		boardStates.splitWorkers(&wg, workers, width, height)
		wg.Wait()
		// we can swap the two boards now, since the old next is current, and we will update the new next fully anyway
		boardStates.current, boardStates.next = boardStates.next, boardStates.current
	}

	// TODO: Report the final state using FinalTurnCompleteEvent.
	aliveCells := boardStates.current.FinalAliveCells()
	c.events <- FinalTurnComplete{turn,aliveCells}

	// Make sure that the Io has finished any output before exiting.
	c.ioCommand <- ioCheckIdle
	<-c.ioIdle

	c.events <- StateChange{turn, Quitting}
	
	// Close the channel to stop the SDL goroutine gracefully. Removing may cause deadlock.
	close(c.events)
}
